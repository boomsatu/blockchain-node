
package security

import (
	"blockchain-node/logger"
	"net"
	"sync"
	"time"
)

type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.RWMutex
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	
	// Clean up old entries periodically
	go rl.cleanup()
	
	return rl
}

func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	
	now := time.Now()
	
	// Get client requests
	requests, exists := rl.requests[clientIP]
	if !exists {
		rl.requests[clientIP] = []time.Time{now}
		return true
	}
	
	// Remove old requests outside the window
	validRequests := make([]time.Time, 0)
	for _, reqTime := range requests {
		if now.Sub(reqTime) <= rl.window {
			validRequests = append(validRequests, reqTime)
		}
	}
	
	// Check if limit exceeded
	if len(validRequests) >= rl.limit {
		logger.LogSecurityEvent("rate_limit_exceeded", map[string]interface{}{
			"client_ip":      clientIP,
			"request_count":  len(validRequests),
			"limit":          rl.limit,
			"window_seconds": rl.window.Seconds(),
		})
		return false
	}
	
	// Add current request
	validRequests = append(validRequests, now)
	rl.requests[clientIP] = validRequests
	
	return true
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		rl.mutex.Lock()
		now := time.Now()
		
		for clientIP, requests := range rl.requests {
			validRequests := make([]time.Time, 0)
			for _, reqTime := range requests {
				if now.Sub(reqTime) <= rl.window {
					validRequests = append(validRequests, reqTime)
				}
			}
			
			if len(validRequests) == 0 {
				delete(rl.requests, clientIP)
			} else {
				rl.requests[clientIP] = validRequests
			}
		}
		
		rl.mutex.Unlock()
	}
}

type SecurityManager struct {
	rateLimiter    *RateLimiter
	blacklistedIPs map[string]time.Time
	mutex          sync.RWMutex
}

func NewSecurityManager() *SecurityManager {
	return &SecurityManager{
		rateLimiter:    NewRateLimiter(100, time.Minute), // 100 requests per minute
		blacklistedIPs: make(map[string]time.Time),
	}
}

func (sm *SecurityManager) IsAllowed(clientIP string) bool {
	sm.mutex.RLock()
	blacklistTime, isBlacklisted := sm.blacklistedIPs[clientIP]
	sm.mutex.RUnlock()
	
	// Check if IP is blacklisted and if blacklist has expired
	if isBlacklisted {
		if time.Since(blacklistTime) < time.Hour {
			logger.LogSecurityEvent("blacklisted_ip_access", map[string]interface{}{
				"client_ip": clientIP,
			})
			return false
		} else {
			// Remove expired blacklist entry
			sm.mutex.Lock()
			delete(sm.blacklistedIPs, clientIP)
			sm.mutex.Unlock()
		}
	}
	
	return sm.rateLimiter.Allow(clientIP)
}

func (sm *SecurityManager) BlacklistIP(clientIP string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.blacklistedIPs[clientIP] = time.Now()
	logger.LogSecurityEvent("ip_blacklisted", map[string]interface{}{
		"client_ip": clientIP,
	})
}

func (sm *SecurityManager) ValidateClientIP(remoteAddr string) string {
	// Extract IP from remote address
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If no port, assume it's just an IP
		host = remoteAddr
	}
	
	// Validate IP format
	ip := net.ParseIP(host)
	if ip == nil {
		logger.Warning("Invalid IP address format: ", host)
		return ""
	}
	
	return ip.String()
}
