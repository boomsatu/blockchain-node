package health

import (
	"blockchain-node/core"
	"blockchain-node/database"
	"blockchain-node/logger"
	"blockchain-node/metrics"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"
)

type HealthStatus struct {
	Status     string                 `json:"status"`
	Timestamp  int64                  `json:"timestamp"`
	Uptime     string                 `json:"uptime"`
	Version    string                 `json:"version"`
	Services   map[string]ServiceInfo `json:"services"`
	Metrics    map[string]interface{} `json:"metrics"`
	SystemInfo SystemInfo             `json:"system_info"`
}

type ServiceInfo struct {
	Status      string `json:"status"`
	LastChecked int64  `json:"last_checked"`
	Message     string `json:"message,omitempty"`
}

type SystemInfo struct {
	GoVersion    string `json:"go_version"`
	NumGoroutine int    `json:"num_goroutine"`
	NumCPU       int    `json:"num_cpu"`
	MemoryMB     uint64 `json:"memory_mb"`
}

type HealthChecker struct {
	blockchain *core.Blockchain
	database   database.Database
	startTime  time.Time
}

func NewHealthChecker(blockchain *core.Blockchain, db database.Database) *HealthChecker {
	return &HealthChecker{
		blockchain: blockchain,
		database:   db,
		startTime:  time.Now(),
	}
}

func (hc *HealthChecker) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().Unix(),
		Uptime:    time.Since(hc.startTime).String(),
		Version:   "1.0.0",
		Services:  make(map[string]ServiceInfo),
		Metrics:   metrics.GetMetrics().ToMap(),
	}

	// Check database
	dbStatus := hc.checkDatabase()
	status.Services["database"] = dbStatus
	if dbStatus.Status != "healthy" {
		status.Status = "degraded"
	}

	// Check blockchain
	blockchainStatus := hc.checkBlockchain()
	status.Services["blockchain"] = blockchainStatus
	if blockchainStatus.Status != "healthy" {
		status.Status = "degraded"
	}

	// System information
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	status.SystemInfo = SystemInfo{
		GoVersion:    runtime.Version(),
		NumGoroutine: runtime.NumGoroutine(),
		NumCPU:       runtime.NumCPU(),
		MemoryMB:     m.Alloc / 1024 / 1024,
	}

	return status
}

func (hc *HealthChecker) checkDatabase() ServiceInfo {
	now := time.Now().Unix()

	if hc.database == nil {
		return ServiceInfo{
			Status:      "unhealthy",
			LastChecked: now,
			Message:     "Database not initialized",
		}
	}

	// Try a simple operation
	_, err := hc.database.Get([]byte("health_check"))
	if err != nil && err.Error() != "key not found" {
		return ServiceInfo{
			Status:      "unhealthy",
			LastChecked: now,
			Message:     "Database connection failed: " + err.Error(),
		}
	}

	return ServiceInfo{
		Status:      "healthy",
		LastChecked: now,
	}
}

func (hc *HealthChecker) checkBlockchain() ServiceInfo {
	now := time.Now().Unix()

	if hc.blockchain == nil {
		return ServiceInfo{
			Status:      "unhealthy",
			LastChecked: now,
			Message:     "Blockchain not initialized",
		}
	}

	currentBlock := hc.blockchain.GetCurrentBlock()
	if currentBlock == nil {
		return ServiceInfo{
			Status:      "unhealthy",
			LastChecked: now,
			Message:     "No current block found",
		}
	}

	return ServiceInfo{
		Status:      "healthy",
		LastChecked: now,
		Message:     fmt.Sprintf("Current block: %x", currentBlock.Header.Hash),
	}
}

func (hc *HealthChecker) HealthHandler(w http.ResponseWriter, r *http.Request) {
	health := hc.CheckHealth()

	w.Header().Set("Content-Type", "application/json")

	// Set HTTP status based on health
	switch health.Status {
	case "healthy":
		w.WriteHeader(http.StatusOK)
	case "degraded":
		w.WriteHeader(http.StatusOK) // Still OK but with warnings
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		logger.Errorf("Failed to encode health response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (hc *HealthChecker) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	// Simple readiness check
	ready := map[string]interface{}{
		"ready":     true,
		"timestamp": time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(ready); err != nil {
		logger.Errorf("Failed to encode readiness response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
