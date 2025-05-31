package rpc

import (
	"blockchain-node/core"
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings" // Ditambahkan untuk TrimPrefix
	"sync"
	"time"
)

type MiningAPI struct {
	blockchain *core.Blockchain
	miner      *core.Miner
	stats      *MiningStats
	mutex      sync.RWMutex
	isActive   bool
}

type MiningStats struct {
	IsActive     bool    `json:"isActive"`
	HashRate     float64 `json:"hashRate"`
	BlocksFound  int     `json:"blocksFound"`
	Difficulty   string  `json:"difficulty"`
	MinerAddress string  `json:"minerAddress"`
	StartTime    int64   `json:"startTime"`
}

func NewMiningAPI(blockchain *core.Blockchain) *MiningAPI {
	return &MiningAPI{
		blockchain: blockchain,
		stats: &MiningStats{
			IsActive:    false,
			HashRate:    0,
			BlocksFound: 0,
			Difficulty:  "1000",
		},
	}
}

func (api *MiningAPI) StartHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		MinerAddress string `json:"minerAddress"`
		Threads      int    `json:"threads"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.MinerAddress) == 0 {
		http.Error(w, "Miner address is required", http.StatusBadRequest)
		return
	}
	minerAddrBytes, err := hex.DecodeString(strings.TrimPrefix(req.MinerAddress, "0x"))
	if err != nil || len(minerAddrBytes) != 20 {
		http.Error(w, "Invalid miner address format", http.StatusBadRequest)
		return
	}
	var minerAddr20 [20]byte
	copy(minerAddr20[:], minerAddrBytes)

	api.mutex.Lock()
	defer api.mutex.Unlock()

	if api.isActive {
		http.Error(w, "Mining already active", http.StatusConflict)
		return
	}

	consensusEngine := api.blockchain.GetConsensusEngine()
	if consensusEngine == nil {
		logger.Error("MiningAPI: Consensus engine not set in blockchain. Cannot start miner.")
		http.Error(w, "Consensus engine not configured", http.StatusInternalServerError)
		return
	}

	// Perbaikan: Pemanggilan core.NewMiner dengan argumen yang benar
	api.miner = core.NewMiner(api.blockchain, minerAddr20, consensusEngine)
	api.isActive = true
	api.stats.IsActive = true
	api.stats.MinerAddress = req.MinerAddress
	api.stats.StartTime = time.Now().Unix()
	api.stats.BlocksFound = 0
	api.stats.HashRate = 0

	go api.miner.Start()

	response := map[string]interface{}{
		"success": true,
		"message": "Mining started successfully",
		"stats":   api.stats,
	}
	json.NewEncoder(w).Encode(response)
}

func (api *MiningAPI) StopHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.mutex.Lock()
	defer api.mutex.Unlock()

	if !api.isActive {
		http.Error(w, "Mining not active", http.StatusConflict)
		return
	}

	if api.miner != nil {
		api.miner.Stop()
	}
	api.isActive = false
	api.stats.IsActive = false
	response := map[string]interface{}{
		"success": true,
		"message": "Mining stopped successfully",
	}
	json.NewEncoder(w).Encode(response)
}

func (api *MiningAPI) StatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	api.mutex.RLock()
	statsCopy := *api.stats // Buat salinan untuk dibaca dengan aman
	api.mutex.RUnlock()

	currentBlock := api.blockchain.GetCurrentBlock()
	if currentBlock != nil {
		statsCopy.Difficulty = currentBlock.Header.GetDifficulty().String()
	}
	// Perhitungan HashRate yang lebih akurat mungkin memerlukan informasi dari miner
	json.NewEncoder(w).Encode(statsCopy)
}

func (api *MiningAPI) MineBlockHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		MinerAddress string `json:"minerAddress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.MinerAddress) == 0 {
		http.Error(w, "Miner address is required", http.StatusBadRequest)
		return
	}
	minerAddrBytes, err := hex.DecodeString(strings.TrimPrefix(req.MinerAddress, "0x"))
	if err != nil || len(minerAddrBytes) != 20 {
		http.Error(w, "Invalid miner address format", http.StatusBadRequest)
		return
	}
	var minerAddr20 [20]byte
	copy(minerAddr20[:], minerAddrBytes)

	parentBlock := api.blockchain.GetCurrentBlock()
	if parentBlock == nil {
		http.Error(w, "Cannot mine block: no current block found", http.StatusInternalServerError)
		return
	}

	transactions := api.blockchain.GetMempool().GetPendingTransactionsForBlock(parentBlock.Header.GetGasLimit())

	consensusEngine := api.blockchain.GetConsensusEngine()
	if consensusEngine == nil {
		http.Error(w, "Consensus engine not configured", http.StatusInternalServerError)
		return
	}

	var grandParentBlock interfaces.BlockConsensusItf = parentBlock
	if parentBlock.Header.GetNumber() > 0 {
		gp := api.blockchain.GetBlockByHash(parentBlock.Header.GetParentHash())
		if gp != nil {
			grandParentBlock = gp
		}
	}
	nextDifficulty := consensusEngine.CalculateDifficulty(parentBlock, grandParentBlock)
	if nextDifficulty.Sign() <= 0 {
		nextDifficulty = new(big.Int).Set(parentBlock.Header.GetDifficulty())
		if nextDifficulty.Sign() <= 0 {
			nextDifficulty = big.NewInt(1000)
		}
	}

	// Perbaikan: Pemanggilan core.NewBlock dengan argumen yang benar
	block := core.NewBlock(
		parentBlock.Header.GetHash(),
		parentBlock.Header.GetNumber()+1,
		minerAddr20,                                   // miner [20]byte
		nextDifficulty,                                // difficulty *big.Int
		api.blockchain.GetConfig().GetBlockGasLimit(), // gasLimit uint64
		transactions,                                  // transactions []*core.Transaction
	)

	// Tambahkan transaksi reward
	rewardValueStr := "2000000000000000000" // 2 ETH
	rewardValue, _ := new(big.Int).SetString(rewardValueStr, 10)
	rewardTx := core.NewTransaction(0, &minerAddr20, rewardValue, 0, big.NewInt(0), nil)
	block.Transactions = append([]*core.Transaction{rewardTx}, block.Transactions...)

	if err := consensusEngine.MineBlock(block); err != nil {
		http.Error(w, "Failed to mine block: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := api.blockchain.AddBlock(block); err != nil {
		http.Error(w, "Failed to add mined block to blockchain: "+err.Error(), http.StatusInternalServerError)
		return
	}

	api.mutex.Lock()
	api.stats.BlocksFound++
	api.mutex.Unlock()

	response := map[string]interface{}{
		"blockNumber": block.Header.GetNumber(),
		"hash":        fmt.Sprintf("0x%x", block.Header.GetHash()),
		"success":     true,
	}
	json.NewEncoder(w).Encode(response)
}
