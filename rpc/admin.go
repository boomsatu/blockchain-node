package rpc

import (
	"blockchain-node/core"
	"encoding/json"
	"net/http"
	"time"
)

type AdminAPI struct {
	blockchain *core.Blockchain
	nodeStatus *NodeStatus
}

type NodeStatus struct {
	Running   bool        `json:"running"`
	StartTime time.Time   `json:"startTime"`
	Config    *NodeConfig `json:"config"`
}

type NodeConfig struct {
	DataDir       string `json:"dataDir"`
	ChainID       uint64 `json:"chainId"`
	Port          int    `json:"port"`
	RPCPort       int    `json:"rpcPort"`
	BlockGasLimit uint64 `json:"blockGasLimit"`
	Mining        bool   `json:"mining"`
	Miner         string `json:"miner"`
}

func NewAdminAPI(blockchain *core.Blockchain) *AdminAPI {
	return &AdminAPI{
		blockchain: blockchain,
		nodeStatus: &NodeStatus{
			Running:   true,
			StartTime: time.Now(),
			Config: &NodeConfig{
				DataDir:       "./data",
				ChainID:       1337,
				Port:          30303,
				RPCPort:       8545,
				BlockGasLimit: 8000000,
				Mining:        false,
				Miner:         "",
			},
		},
	}
}

func (api *AdminAPI) StartHandler(w http.ResponseWriter, r *http.Request) {
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

	var config NodeConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		// Use default config if no config provided
		config = *api.nodeStatus.Config
	}

	// Update node config
	api.nodeStatus.Config = &config
	api.nodeStatus.Running = true
	api.nodeStatus.StartTime = time.Now()

	response := map[string]interface{}{
		"success": true,
		"message": "Node started successfully",
		"status":  api.nodeStatus,
	}

	json.NewEncoder(w).Encode(response)
}

func (api *AdminAPI) StopHandler(w http.ResponseWriter, r *http.Request) {
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

	api.nodeStatus.Running = false

	response := map[string]interface{}{
		"success": true,
		"message": "Node stopped successfully",
	}

	json.NewEncoder(w).Encode(response)
}

func (api *AdminAPI) StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	response := map[string]interface{}{
		"status":    "ok",
		"config":    api.nodeStatus.Config,
		"running":   api.nodeStatus.Running,
		"startTime": api.nodeStatus.StartTime.Unix(),
		"uptime":    time.Since(api.nodeStatus.StartTime).Seconds(),
	}

	json.NewEncoder(w).Encode(response)
}

func (api *AdminAPI) ConfigHandler(w http.ResponseWriter, r *http.Request) {
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

	var newConfig NodeConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "Invalid config format", http.StatusBadRequest)
		return
	}

	// Update configuration
	api.nodeStatus.Config = &newConfig

	response := map[string]interface{}{
		"success": true,
		"message": "Configuration updated successfully",
		"config":  api.nodeStatus.Config,
	}

	json.NewEncoder(w).Encode(response)
}
