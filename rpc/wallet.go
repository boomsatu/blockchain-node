package rpc

import (
	"blockchain-node/core"
	"blockchain-node/wallet"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
)

type WalletAPI struct {
	blockchain *core.Blockchain
}

func NewWalletAPI(blockchain *core.Blockchain) *WalletAPI {
	return &WalletAPI{
		blockchain: blockchain,
	}
}

func (api *WalletAPI) CreateHandler(w http.ResponseWriter, r *http.Request) {
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

	newWallet, err := wallet.NewWallet()
	if err != nil {
		http.Error(w, "Failed to create wallet: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get balance (should be 0 for new wallet)
	address := newWallet.GetAddressBytes()
	balance := api.blockchain.GetStateDB().GetBalance(address)

	response := map[string]interface{}{
		"address":    "0x" + newWallet.GetAddress(),
		"privateKey": "0x" + newWallet.GetPrivateKeyHex(),
		"balance":    fmt.Sprintf("0x%x", balance),
	}

	json.NewEncoder(w).Encode(response)
}

func (api *WalletAPI) ImportHandler(w http.ResponseWriter, r *http.Request) {
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
		PrivateKey string `json:"privateKey"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Remove 0x prefix if present
	privateKeyHex := req.PrivateKey
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	importedWallet, err := wallet.NewWalletFromPrivateKey(privateKeyHex)
	if err != nil {
		http.Error(w, "Failed to import wallet: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get balance
	address := importedWallet.GetAddressBytes()
	balance := api.blockchain.GetStateDB().GetBalance(address)

	response := map[string]interface{}{
		"address":    "0x" + importedWallet.GetAddress(),
		"privateKey": importedWallet.GetPrivateKeyHex(),
		"balance":    fmt.Sprintf("0x%x", balance),
	}

	json.NewEncoder(w).Encode(response)
}

func (api *WalletAPI) SendTransactionHandler(w http.ResponseWriter, r *http.Request) {
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
		From       string `json:"from"`
		To         string `json:"to"`
		Value      string `json:"value"`
		GasLimit   string `json:"gasLimit"`
		GasPrice   string `json:"gasPrice"`
		PrivateKey string `json:"privateKey"`
		Data       string `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Import wallet from private key
	privateKeyHex := req.PrivateKey
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	senderWallet, err := wallet.NewWalletFromPrivateKey(privateKeyHex)
	if err != nil {
		http.Error(w, "Invalid private key", http.StatusBadRequest)
		return
	}

	// Parse values
	value, ok := new(big.Int).SetString(req.Value, 0)
	if !ok {
		http.Error(w, "Invalid value format", http.StatusBadRequest)
		return
	}

	gasLimit, ok := new(big.Int).SetString(req.GasLimit, 0)
	if !ok {
		gasLimit = big.NewInt(21000) // Default gas limit
	}

	gasPrice, ok := new(big.Int).SetString(req.GasPrice, 0)
	if !ok {
		gasPrice = big.NewInt(20000000000) // Default 20 Gwei
	}

	// Parse to address
	var toAddr *[20]byte
	if req.To != "" {
		toAddrStr := req.To
		if len(toAddrStr) > 2 && toAddrStr[:2] == "0x" {
			toAddrStr = toAddrStr[2:]
		}

		if len(toAddrStr) == 40 {
			var addr [20]byte
			for i := 0; i < 20; i++ {
				fmt.Sscanf(toAddrStr[i*2:i*2+2], "%02x", &addr[i])
			}
			toAddr = &addr
		}
	}

	// Get nonce
	fromAddr := senderWallet.GetAddressBytes()
	nonce := api.blockchain.GetStateDB().GetNonce(fromAddr)

	// Parse data
	var data []byte
	if req.Data != "" {
		dataStr := req.Data
		if len(dataStr) > 2 && dataStr[:2] == "0x" {
			dataStr = dataStr[2:]
		}
		if len(dataStr)%2 == 0 {
			data = make([]byte, len(dataStr)/2)
			for i := 0; i < len(data); i++ {
				fmt.Sscanf(dataStr[i*2:i*2+2], "%02x", &data[i])
			}
		}
	}

	// Create transaction
	tx := core.NewTransaction(nonce, toAddr, value, gasLimit.Uint64(), gasPrice, data)

	// Sign transaction
	if err := senderWallet.SignTransaction(tx); err != nil {
		http.Error(w, "Failed to sign transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add to mempool
	if err := api.blockchain.GetMempool().AddTransaction(tx); err != nil {
		http.Error(w, "Failed to add transaction to mempool: "+err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"hash":    fmt.Sprintf("0x%x", tx.Hash),
		"success": true,
	}

	json.NewEncoder(w).Encode(response)
}
