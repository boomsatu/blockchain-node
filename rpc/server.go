package rpc

import (
	"blockchain-node/core"
	"blockchain-node/security" // Asumsi ada package ini
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	// "blockchain-node/logger" // Jika diperlukan untuk logging
)

type Config struct {
	Host string
	Port int
}

type Server struct {
	config     *Config
	blockchain *core.Blockchain
	security   *security.SecurityManager // Asumsi ada
	server     *http.Server
	adminAPI   *AdminAPI
	miningAPI  *MiningAPI
	walletAPI  *WalletAPI
	networkAPI *NetworkAPI
}

type JSONRPCRequest struct {
	ID      interface{}   `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	Version string        `json:"jsonrpc"`
}

type JSONRPCResponse struct {
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	Version string        `json:"jsonrpc"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewServer(config *Config, blockchain *core.Blockchain) *Server {
	// Inisialisasi securityManager jika belum ada
	secManager := security.NewSecurityManager() // Atau dapatkan dari parameter jika diinisialisasi di tempat lain

	return &Server{
		config:     config,
		blockchain: blockchain,
		security:   secManager, // Menggunakan instance yang diinisialisasi
		adminAPI:   NewAdminAPI(blockchain),
		miningAPI:  NewMiningAPI(blockchain),
		walletAPI:  NewWalletAPI(blockchain),
		networkAPI: NewNetworkAPI(blockchain),
	}
}

func (s *Server) Start() error {
	router := mux.NewRouter()

	router.HandleFunc("/", s.handleRPC).Methods("POST", "OPTIONS") // OPTIONS untuk CORS preflight
	router.HandleFunc("/health", s.handleHealth).Methods("GET", "OPTIONS")

	api := router.PathPrefix("/api").Subrouter()

	admin := api.PathPrefix("/admin").Subrouter()
	admin.HandleFunc("/start", s.adminAPI.StartHandler).Methods("POST", "OPTIONS")
	admin.HandleFunc("/stop", s.adminAPI.StopHandler).Methods("POST", "OPTIONS")
	admin.HandleFunc("/status", s.adminAPI.StatusHandler).Methods("GET", "OPTIONS")
	admin.HandleFunc("/config", s.adminAPI.ConfigHandler).Methods("POST", "OPTIONS")

	mining := api.PathPrefix("/mining").Subrouter()
	mining.HandleFunc("/start", s.miningAPI.StartHandler).Methods("POST", "OPTIONS")
	mining.HandleFunc("/stop", s.miningAPI.StopHandler).Methods("POST", "OPTIONS")
	mining.HandleFunc("/stats", s.miningAPI.StatsHandler).Methods("GET", "OPTIONS")
	mining.HandleFunc("/mine-block", s.miningAPI.MineBlockHandler).Methods("POST", "OPTIONS")

	walletRouter := api.PathPrefix("/wallet").Subrouter()
	walletRouter.HandleFunc("/create", s.walletAPI.CreateHandler).Methods("POST", "OPTIONS")
	walletRouter.HandleFunc("/import", s.walletAPI.ImportHandler).Methods("POST", "OPTIONS")
	walletRouter.HandleFunc("/send", s.walletAPI.SendTransactionHandler).Methods("POST", "OPTIONS")

	network := api.PathPrefix("/network").Subrouter()
	network.HandleFunc("/stats", s.networkAPI.StatsHandler).Methods("GET", "OPTIONS")
	network.HandleFunc("/peers", s.networkAPI.PeersHandler).Methods("GET", "OPTIONS")

	api.HandleFunc("/metrics", s.networkAPI.MetricsHandler).Methods("GET", "OPTIONS")

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: router, // Menggunakan router yang sudah dikonfigurasi
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("RPC server error: %v\n", err) // Sebaiknya gunakan logger
		}
	}()

	fmt.Printf("JSON-RPC server with REST API started on %s\n", addr) // Sebaiknya gunakan logger
	return nil
}

func (s *Server) Stop() {
	if s.server != nil {
		s.server.Close()
		fmt.Println("JSON-RPC server stopped") // Sebaiknya gunakan logger
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // CORS header
	if r.Method == "OPTIONS" {                         // Handle preflight
		w.Header().Set("Access-Control-Allow-Methods", "GET")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, nil, -32700, "Parse error")
		return
	}

	result, err := s.handleMethod(req.Method, req.Params)
	if err != nil {
		s.sendError(w, req.ID, -32603, err.Error()) // Kode error internal
		return
	}

	response := JSONRPCResponse{
		ID:      req.ID,
		Result:  result,
		Version: "2.0",
	}
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleMethod(method string, params []interface{}) (interface{}, error) {
	switch method {
	case "eth_blockNumber":
		return s.ethBlockNumber()
	case "eth_getBalance":
		return s.ethGetBalance(params)
	case "eth_getBlockByNumber":
		return s.ethGetBlockByNumber(params)
	case "eth_getBlockByHash":
		return s.ethGetBlockByHash(params)
	case "eth_getTransactionByHash":
		return s.ethGetTransactionByHash(params)
	case "eth_getTransactionReceipt":
		return s.ethGetTransactionReceipt(params)
	case "eth_sendRawTransaction":
		return s.ethSendRawTransaction(params)
	case "eth_call":
		return s.ethCall(params)
	case "eth_estimateGas":
		return s.ethEstimateGas(params)
	case "eth_gasPrice":
		return s.ethGasPrice()
	case "eth_chainId":
		// Menggunakan metode getter dari interface ChainConfigItf
		return fmt.Sprintf("0x%x", s.blockchain.GetConfig().GetChainID()), nil
	case "eth_getTransactionCount":
		return s.ethGetTransactionCount(params)
	case "eth_getCode":
		return s.ethGetCode(params)
	case "eth_getStorageAt":
		return s.ethGetStorageAt(params)
	case "eth_getLogs":
		return s.ethGetLogs(params)
	case "net_version":
		// Menggunakan metode getter dari interface ChainConfigItf
		return strconv.FormatUint(s.blockchain.GetConfig().GetChainID(), 10), nil
	case "web3_clientVersion":
		return "blockchain-node/1.0.0", nil
	default:
		return nil, fmt.Errorf("method not found: %s", method)
	}
}

// ... (sisa implementasi metode eth_ lainnya tetap sama, pastikan mereka menggunakan getter jika mengakses config) ...
func (s *Server) ethBlockNumber() (interface{}, error) {
	currentBlock := s.blockchain.GetCurrentBlock()
	if currentBlock == nil {
		return "0x0", nil
	}
	return fmt.Sprintf("0x%x", currentBlock.Header.GetNumber()), nil
}

func (s *Server) ethGetBalance(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing address parameter")
	}
	addressStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("address parameter must be a string")
	}
	addressBytes, err := hex.DecodeString(strings.TrimPrefix(addressStr, "0x"))
	if err != nil || len(addressBytes) != 20 {
		return nil, fmt.Errorf("invalid address format")
	}
	var address [20]byte
	copy(address[:], addressBytes)
	balance := s.blockchain.GetBalance(address) // Menggunakan GetBalance dari Blockchain
	return fmt.Sprintf("0x%x", balance), nil
}

func (s *Server) ethGetBlockByNumber(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing block number parameter")
	}
	blockNumStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("block number parameter must be a string")
	}
	var blockNum uint64
	var err error
	if blockNumStr == "latest" {
		if block := s.blockchain.GetCurrentBlock(); block != nil {
			blockNum = block.Header.GetNumber()
		} else {
			return nil, nil // Atau error jika tidak ada blok
		}
	} else {
		blockNum, err = strconv.ParseUint(strings.TrimPrefix(blockNumStr, "0x"), 16, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid block number: %v", err)
		}
	}
	block := s.blockchain.GetBlockByNumber(blockNum)
	if block == nil {
		return nil, nil
	}
	fullTx := false
	if len(params) > 1 {
		if val, ok := params[1].(bool); ok {
			fullTx = val
		}
	}
	return s.formatBlock(block, fullTx), nil
}

func (s *Server) ethGetBlockByHash(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing block hash parameter")
	}
	hashStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("block hash parameter must be a string")
	}
	hashBytes, err := hex.DecodeString(strings.TrimPrefix(hashStr, "0x"))
	if err != nil || len(hashBytes) != 32 {
		return nil, fmt.Errorf("invalid hash format")
	}
	var hash [32]byte
	copy(hash[:], hashBytes)
	block := s.blockchain.GetBlockByHash(hash)
	if block == nil {
		return nil, nil
	}
	fullTx := false
	if len(params) > 1 {
		if val, ok := params[1].(bool); ok {
			fullTx = val
		}
	}
	return s.formatBlock(block, fullTx), nil
}

func (s *Server) ethGetTransactionByHash(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing transaction hash parameter")
	}
	hashStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("transaction hash parameter must be a string")
	}
	hashBytes, err := hex.DecodeString(strings.TrimPrefix(hashStr, "0x"))
	if err != nil || len(hashBytes) != 32 {
		return nil, fmt.Errorf("invalid hash format")
	}
	var hash [32]byte
	copy(hash[:], hashBytes)

	if tx := s.blockchain.GetMempool().GetTransaction(hash); tx != nil {
		return s.formatTransaction(tx, nil, 0, 0), nil
	}

	// Cari di blok yang sudah di-commit
	// Ini bisa menjadi operasi yang mahal jika harus iterasi semua blok
	// Idealnya ada indeks txHash -> blockHash/Number
	// Untuk sekarang, kita asumsikan transaksi hanya dicari di mempool atau tidak ada.
	// Implementasi yang lebih lengkap akan mencari di database blok.
	return nil, nil // Placeholder untuk pencarian di blok
}

func (s *Server) ethGetTransactionReceipt(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing transaction hash parameter")
	}
	hashStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("transaction hash parameter must be a string")
	}
	hashBytes, err := hex.DecodeString(strings.TrimPrefix(hashStr, "0x"))
	if err != nil || len(hashBytes) != 32 {
		return nil, fmt.Errorf("invalid hash format")
	}
	var txHash [32]byte
	copy(txHash[:], hashBytes)

	// Iterasi blok untuk mencari receipt. Ini tidak efisien untuk produksi.
	// Anda memerlukan indeks txHash -> receipt atau txHash -> block.
	currentNum := s.blockchain.GetCurrentBlock().Header.GetNumber()
	for i := currentNum; i >= 0; i-- { // Iterasi mundur
		block := s.blockchain.GetBlockByNumber(i)
		if block == nil {
			continue
		}
		for txIdx, tx := range block.Transactions {
			if tx.GetHash() == txHash {
				if txIdx < len(block.Receipts) {
					return s.formatReceipt(block.Receipts[txIdx]), nil
				}
				return nil, fmt.Errorf("receipt found but index out of bounds for tx %x in block %d", txHash, i)
			}
		}
		if i == 0 { // Hentikan jika sudah mencapai genesis
			break
		}
	}
	return nil, nil // Receipt tidak ditemukan
}

func (s *Server) ethSendRawTransaction(params []interface{}) (interface{}, error) {
	// Implementasi ini memerlukan parsing raw transaction, verifikasi, dan penambahan ke mempool
	return nil, fmt.Errorf("eth_sendRawTransaction not fully implemented")
}

func (s *Server) ethCall(params []interface{}) (interface{}, error) {
	// Implementasi ini memerlukan eksekusi transaksi secara read-only
	return nil, fmt.Errorf("eth_call not fully implemented")
}

func (s *Server) ethEstimateGas(params []interface{}) (interface{}, error) {
	// Implementasi ini memerlukan simulasi eksekusi untuk estimasi gas
	// txData := params[0].(map[string]interface{}) // Perlu parsing yang lebih baik
	// tx := core.NewTransaction(...) // Buat transaksi dari params
	// gas, err := s.blockchain.EstimateGas(tx)
	// if err != nil { return nil, err }
	// return fmt.Sprintf("0x%x", gas), nil
	return fmt.Sprintf("0x%x", uint64(21000)), nil // Placeholder
}

func (s *Server) ethGasPrice() (interface{}, error) {
	return fmt.Sprintf("0x%x", big.NewInt(20000000000)), nil // 20 Gwei
}

func (s *Server) ethGetTransactionCount(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing address parameter")
	}
	addressStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("address parameter must be a string")
	}
	addressBytes, err := hex.DecodeString(strings.TrimPrefix(addressStr, "0x"))
	if err != nil || len(addressBytes) != 20 {
		return nil, fmt.Errorf("invalid address format")
	}
	var address [20]byte
	copy(address[:], addressBytes)
	nonce := s.blockchain.GetNonce(address) // Menggunakan GetNonce dari Blockchain
	return fmt.Sprintf("0x%x", nonce), nil
}

func (s *Server) ethGetCode(params []interface{}) (interface{}, error) {
	if len(params) < 1 {
		return nil, fmt.Errorf("missing address parameter")
	}
	addressStr, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("address parameter must be a string")
	}
	addressBytes, err := hex.DecodeString(strings.TrimPrefix(addressStr, "0x"))
	if err != nil || len(addressBytes) != 20 {
		return nil, fmt.Errorf("invalid address format")
	}
	var address [20]byte
	copy(address[:], addressBytes)
	code := s.blockchain.GetCode(address) // Menggunakan GetCode dari Blockchain
	return "0x" + hex.EncodeToString(code), nil
}

func (s *Server) ethGetStorageAt(params []interface{}) (interface{}, error) {
	if len(params) < 2 {
		return nil, fmt.Errorf("eth_getStorageAt requires address and position parameters")
	}
	addressStr, okA := params[0].(string)
	positionStr, okP := params[1].(string)
	if !okA || !okP {
		return nil, fmt.Errorf("address and position parameters must be strings")
	}

	addressBytes, err := hex.DecodeString(strings.TrimPrefix(addressStr, "0x"))
	if err != nil || len(addressBytes) != 20 {
		return nil, fmt.Errorf("invalid address format")
	}
	var address [20]byte
	copy(address[:], addressBytes)

	positionBytes, err := hex.DecodeString(strings.TrimPrefix(positionStr, "0x"))
	if err != nil { // Panjang bisa bervariasi, tapi biasanya 32 byte untuk key storage
		return nil, fmt.Errorf("invalid storage position format")
	}
	var positionKey [32]byte // Storage keys adalah 32 byte
	// Jika positionBytes lebih pendek, kita pad dengan nol di depan (atau sesuai konvensi Ethereum)
	copy(positionKey[32-len(positionBytes):], positionBytes)

	value := s.blockchain.GetStorageAt(address, positionKey)
	return "0x" + hex.EncodeToString(value[:]), nil
}

func (s *Server) ethGetLogs(params []interface{}) (interface{}, error) {
	// Implementasi ini memerlukan query log dari blok atau database
	return []interface{}{}, nil // Placeholder
}

func (s *Server) formatBlock(block *core.Block, fullTx bool) map[string]interface{} {
	header := block.Header // Ini adalah *core.BlockHeader
	result := map[string]interface{}{
		"number":           fmt.Sprintf("0x%x", header.GetNumber()),
		"hash":             fmt.Sprintf("0x%x", header.GetHash()),
		"parentHash":       fmt.Sprintf("0x%x", header.GetParentHash()),
		"timestamp":        fmt.Sprintf("0x%x", header.GetTimestamp()),
		"stateRoot":        fmt.Sprintf("0x%x", header.StateRoot), // Akses langsung jika ada di struct
		"transactionsRoot": fmt.Sprintf("0x%x", header.TxHash),
		"receiptsRoot":     fmt.Sprintf("0x%x", header.ReceiptHash),
		"gasLimit":         fmt.Sprintf("0x%x", header.GetGasLimit()),
		"gasUsed":          fmt.Sprintf("0x%x", header.GasUsed), // Akses langsung
		"difficulty":       fmt.Sprintf("0x%x", header.GetDifficulty()),
		"nonce":            fmt.Sprintf("0x%x", header.GetNonce()),
		"miner":            fmt.Sprintf("0x%x", header.GetMiner()),
		// "size":             fmt.Sprintf("0x%x", block.Size()), // Anda perlu metode Size() di *core.Block
	}

	if fullTx {
		var transactions []interface{}
		for i, tx := range block.Transactions {
			transactions = append(transactions, s.formatTransaction(tx, &block.Header.Hash, block.Header.Number, uint64(i)))
		}
		result["transactions"] = transactions
	} else {
		var txHashes []string
		for _, tx := range block.Transactions {
			txHashes = append(txHashes, fmt.Sprintf("0x%x", tx.GetHash()))
		}
		result["transactions"] = txHashes
	}
	return result
}

func (s *Server) formatTransaction(tx *core.Transaction, blockHash *[32]byte, blockNumber uint64, txIndex uint64) map[string]interface{} {
	result := map[string]interface{}{
		"hash":     fmt.Sprintf("0x%x", tx.GetHash()),
		"nonce":    fmt.Sprintf("0x%x", tx.GetNonce()),
		"gasPrice": fmt.Sprintf("0x%x", tx.GetGasPrice()),
		"gas":      fmt.Sprintf("0x%x", tx.GetGasLimit()),
		"value":    fmt.Sprintf("0x%x", tx.GetValue()),
		"input":    "0x" + hex.EncodeToString(tx.GetData()),
		"from":     fmt.Sprintf("0x%x", tx.GetFrom()),
		"v":        fmt.Sprintf("0x%x", tx.V), // Akses langsung V,R,S dari struct core.Transaction
		"r":        fmt.Sprintf("0x%x", tx.R),
		"s":        fmt.Sprintf("0x%x", tx.S),
	}
	if txTo := tx.GetTo(); txTo != nil {
		result["to"] = fmt.Sprintf("0x%x", *txTo)
	} else {
		result["to"] = nil
	}
	if blockHash != nil {
		result["blockHash"] = fmt.Sprintf("0x%x", *blockHash)
		result["blockNumber"] = fmt.Sprintf("0x%x", blockNumber)
		result["transactionIndex"] = fmt.Sprintf("0x%x", txIndex)
	} else {
		result["blockHash"] = nil
		result["blockNumber"] = nil
		result["transactionIndex"] = nil
	}
	return result
}

func (s *Server) formatReceipt(receipt *core.TransactionReceipt) map[string]interface{} {
	logs := make([]map[string]interface{}, len(receipt.Logs))
	for i, log := range receipt.Logs {
		topics := make([]string, len(log.Topics))
		for j, t := range log.Topics {
			topics[j] = fmt.Sprintf("0x%x", t)
		}
		logs[i] = map[string]interface{}{
			"address":          fmt.Sprintf("0x%x", log.Address),
			"topics":           topics,
			"data":             "0x" + hex.EncodeToString(log.Data),
			"blockNumber":      fmt.Sprintf("0x%x", log.BlockNumber),
			"transactionHash":  fmt.Sprintf("0x%x", log.TxHash),
			"transactionIndex": fmt.Sprintf("0x%x", log.TxIndex),
			"blockHash":        fmt.Sprintf("0x%x", log.BlockHash),
			"logIndex":         fmt.Sprintf("0x%x", log.Index),
			"removed":          log.Removed,
		}
	}

	result := map[string]interface{}{
		"transactionHash":   fmt.Sprintf("0x%x", receipt.TxHash),
		"transactionIndex":  fmt.Sprintf("0x%x", receipt.TxIndex),
		"blockHash":         fmt.Sprintf("0x%x", receipt.BlockHash),
		"blockNumber":       fmt.Sprintf("0x%x", receipt.BlockNumber),
		"from":              fmt.Sprintf("0x%x", receipt.From),
		"gasUsed":           fmt.Sprintf("0x%x", receipt.GasUsed),
		"cumulativeGasUsed": fmt.Sprintf("0x%x", receipt.CumulativeGasUsed),
		"logs":              logs,
		"status":            fmt.Sprintf("0x%x", receipt.Status),
		"logsBloom":         "0x" + hex.EncodeToString(receipt.LogsBloom),
	}
	if receipt.To != nil {
		result["to"] = fmt.Sprintf("0x%x", *receipt.To)
	} else {
		result["to"] = nil
	}
	if receipt.ContractAddress != nil {
		result["contractAddress"] = fmt.Sprintf("0x%x", *receipt.ContractAddress)
	} else {
		result["contractAddress"] = nil
	}
	return result
}

func (s *Server) sendError(w http.ResponseWriter, id interface{}, code int, message string) {
	response := JSONRPCResponse{
		ID:      id,
		Error:   &JSONRPCError{Code: code, Message: message},
		Version: "2.0",
	}
	json.NewEncoder(w).Encode(response)
}
