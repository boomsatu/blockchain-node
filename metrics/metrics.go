
package metrics

import (
	"sync"
	"time"
)

type Metrics struct {
	TransactionCount     uint64
	BlockCount          uint64
	PeerCount           uint32
	TotalHashRate       uint64
	NetworkLatency      time.Duration
	MemoryUsage         uint64
	DiskUsage           uint64
	ConnectionCount     uint32
	ErrorCount          uint64
	StartTime           time.Time
	LastBlockTime       time.Time
	TransactionPool     uint32
	mutex               sync.RWMutex
}

var globalMetrics *Metrics

func init() {
	globalMetrics = &Metrics{
		StartTime: time.Now(),
	}
}

func GetMetrics() *Metrics {
	return globalMetrics
}

func (m *Metrics) IncrementTransactionCount() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.TransactionCount++
}

func (m *Metrics) IncrementBlockCount() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.BlockCount++
	m.LastBlockTime = time.Now()
}

func (m *Metrics) SetPeerCount(count uint32) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.PeerCount = count
}

func (m *Metrics) SetHashRate(hashRate uint64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.TotalHashRate = hashRate
}

func (m *Metrics) SetNetworkLatency(latency time.Duration) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.NetworkLatency = latency
}

func (m *Metrics) SetMemoryUsage(usage uint64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.MemoryUsage = usage
}

func (m *Metrics) SetDiskUsage(usage uint64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.DiskUsage = usage
}

func (m *Metrics) SetConnectionCount(count uint32) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.ConnectionCount = count
}

func (m *Metrics) IncrementErrorCount() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.ErrorCount++
}

func (m *Metrics) SetTransactionPoolSize(size uint32) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.TransactionPool = size
}

func (m *Metrics) GetUptime() time.Duration {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return time.Since(m.StartTime)
}

func (m *Metrics) GetBlocksPerSecond() float64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	uptime := time.Since(m.StartTime)
	if uptime.Seconds() == 0 {
		return 0
	}
	return float64(m.BlockCount) / uptime.Seconds()
}

func (m *Metrics) GetTransactionsPerSecond() float64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	uptime := time.Since(m.StartTime)
	if uptime.Seconds() == 0 {
		return 0
	}
	return float64(m.TransactionCount) / uptime.Seconds()
}

func (m *Metrics) ToMap() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	return map[string]interface{}{
		"transaction_count":     m.TransactionCount,
		"block_count":          m.BlockCount,
		"peer_count":           m.PeerCount,
		"total_hash_rate":      m.TotalHashRate,
		"network_latency_ms":   m.NetworkLatency.Milliseconds(),
		"memory_usage_mb":      m.MemoryUsage / 1024 / 1024,
		"disk_usage_mb":        m.DiskUsage / 1024 / 1024,
		"connection_count":     m.ConnectionCount,
		"error_count":          m.ErrorCount,
		"uptime_seconds":       time.Since(m.StartTime).Seconds(),
		"blocks_per_second":    m.GetBlocksPerSecond(),
		"transactions_per_second": m.GetTransactionsPerSecond(),
		"transaction_pool_size": m.TransactionPool,
		"last_block_time":      m.LastBlockTime.Unix(),
	}
}
