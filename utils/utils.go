
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// GenerateRandomBytes generates cryptographically secure random bytes
func GenerateRandomBytes(length int) ([]byte, error) {
	bytes := make([]byte, length)
	_, err := rand.Read(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %v", err)
	}
	return bytes, nil
}

// GenerateRandomHex generates a random hex string of specified length
func GenerateRandomHex(length int) (string, error) {
	bytes, err := GenerateRandomBytes(length / 2)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// IsValidHex checks if a string is valid hexadecimal
func IsValidHex(s string) bool {
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// FormatBytes formats byte count as human readable string
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatDuration formats duration as human readable string
func FormatDuration(d time.Duration) string {
	if d.Hours() >= 24 {
		days := int(d.Hours() / 24)
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if d.Hours() >= 1 {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	if d.Minutes() >= 1 {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// GetMemoryUsage returns current memory usage
func GetMemoryUsage() (uint64, uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc, m.Sys
}

// SafeString safely converts bytes to string, replacing invalid UTF-8
func SafeString(data []byte) string {
	return strings.ToValidUTF8(string(data), "�")
}

// MinUint64 returns the minimum of two uint64 values
func MinUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// MaxUint64 returns the maximum of two uint64 values
func MaxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// RetryWithBackoff executes a function with exponential backoff
func RetryWithBackoff(fn func() error, maxRetries int, baseDelay time.Duration) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if err := fn(); err != nil {
			lastErr = err
			if i < maxRetries-1 {
				delay := baseDelay * time.Duration(1<<uint(i)) // Exponential backoff
				time.Sleep(delay)
			}
		} else {
			return nil
		}
	}
	return fmt.Errorf("operation failed after %d retries: %v", maxRetries, lastErr)
}

// ToHex converts bytes to hex string with 0x prefix
func ToHex(data []byte) string {
	return "0x" + hex.EncodeToString(data)
}

// FromHex converts hex string to bytes (with or without 0x prefix)
func FromHex(s string) ([]byte, error) {
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
	}
	return hex.DecodeString(s)
}
