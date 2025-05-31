package rpc

import (
	"blockchain-node/core"
	"encoding/json"
	"net/http"
	"runtime"
	"time"
	// "blockchain-node/logger"
	// "blockchain-node/metrics"
)

type NetworkAPI struct {
	blockchain *core.Blockchain
}

func NewNetworkAPI(blockchain *core.Blockchain) *NetworkAPI {
	return &NetworkAPI{
		blockchain: blockchain,
	}
}

func (api *NetworkAPI) StatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS") // Tambahkan OPTIONS
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type") // Tambahkan Headers

	if r.Method == "OPTIONS" { // Handle preflight
		w.WriteHeader(http.StatusOK)
		return
	}

	currentBlock := api.blockchain.GetCurrentBlock()
	blockHeight := uint64(0)
	var difficulty string = "0"
	if currentBlock != nil {
		blockHeight = currentBlock.Header.GetNumber()
		difficulty = currentBlock.Header.GetDifficulty().String()
	}

	// Menggunakan metode getter dari interface ChainConfigItf
	chainConfig := api.blockchain.GetConfig() // Ini mengembalikan interfaces.ChainConfigItf
	chainID := chainConfig.GetChainID()       // Panggil metode pada interface

	stats := map[string]interface{}{
		"peerCount":   0,
		"blockHeight": blockHeight,
		"difficulty":  difficulty,
		"hashRate":    "0",
		"chainId":     chainID,
		"syncStatus": map[string]interface{}{
			"isSyncing":    false,
			"currentBlock": blockHeight,
			"highestBlock": blockHeight,
		},
	}
	json.NewEncoder(w).Encode(stats)
}

func (api *NetworkAPI) PeersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS") // Tambahkan OPTIONS
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type") // Tambahkan Headers

	if r.Method == "OPTIONS" { // Handle preflight
		w.WriteHeader(http.StatusOK)
		return
	}

	peers := []map[string]interface{}{}
	json.NewEncoder(w).Encode(peers)
}

func (api *NetworkAPI) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS") // Tambahkan OPTIONS
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type") // Tambahkan Headers

	if r.Method == "OPTIONS" { // Handle preflight
		w.WriteHeader(http.StatusOK)
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	currentBlock := api.blockchain.GetCurrentBlock()
	blockCount := uint64(0)
	if currentBlock != nil {
		blockCount = currentBlock.Header.GetNumber() + 1
	}

	transactionCount := uint64(0) // Placeholder

	// Menggunakan metode getter dari interface ChainConfigItf
	chainConfig := api.blockchain.GetConfig()       // Ini mengembalikan interfaces.ChainConfigItf
	blockGasLimit := chainConfig.GetBlockGasLimit() // Panggil metode pada interface

	metricsData := map[string]interface{}{
		"uptime":           time.Now().Unix(), // Seharusnya durasi, bukan timestamp
		"memoryUsage":      m.Alloc,
		"diskUsage":        0,
		"cpuUsage":         0,
		"blockCount":       blockCount,
		"transactionCount": transactionCount,
		"peersConnected":   0,
		"gasUsed":          0,
		"gasLimit":         blockGasLimit,
		"pendingTxs":       len(api.blockchain.GetMempool().GetPendingTransactions()),
	}
	json.NewEncoder(w).Encode(metricsData)
}
