package core

import (
	"blockchain-node/cache"
	"blockchain-node/crypto"
	"blockchain-node/database"
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"blockchain-node/metrics"
	"blockchain-node/state"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

type Config struct {
	DataDir       string
	ChainID       uint64
	BlockGasLimit uint64
}

func (c *Config) ToEthChainConfig() *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             big.NewInt(int64(c.ChainID)),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}
}
func (c *Config) GetChainID() uint64       { return c.ChainID }
func (c *Config) GetBlockGasLimit() uint64 { return c.BlockGasLimit }

type TransactionReceipt struct {
	TxHash            [32]byte  `json:"transactionHash"`
	TxIndex           uint64    `json:"transactionIndex"`
	BlockHash         [32]byte  `json:"blockHash"`
	BlockNumber       uint64    `json:"blockNumber"`
	From              [20]byte  `json:"from"`
	To                *[20]byte `json:"to"`
	GasUsed           uint64    `json:"gasUsed"`
	CumulativeGasUsed uint64    `json:"cumulativeGasUsed"`
	ContractAddress   *[20]byte `json:"contractAddress,omitempty"`
	Logs              []*Log    `json:"logs"`
	Status            uint64    `json:"status"`
	LogsBloom         []byte    `json:"logsBloom,omitempty"`
}

func (tr *TransactionReceipt) ToJSON() ([]byte, error) {
	return json.Marshal(tr)
}

type Log struct {
	Address     [20]byte   `json:"address"`
	Topics      [][32]byte `json:"topics"`
	Data        []byte     `json:"data"`
	BlockNumber uint64     `json:"blockNumber"`
	TxHash      [32]byte   `json:"transactionHash"`
	TxIndex     uint64     `json:"transactionIndex"`
	BlockHash   [32]byte   `json:"blockHash"`
	Index       uint64     `json:"logIndex"`
	Removed     bool       `json:"removed"`
}

type Blockchain struct {
	config          *Config
	db              database.Database
	stateDB         *state.StateDB
	currentBlock    *Block
	blocks          map[[32]byte]*Block
	blockByNumber   map[uint64]*Block
	mempool         *Mempool
	vm              interfaces.VirtualMachine
	consensus       interfaces.Engine
	validator       *Validator
	cache           *cache.Cache
	mu              sync.RWMutex
	shutdownCh      chan struct{}
	totalDifficulty *big.Int // Field untuk menyimpan Total Difficulty
}

func NewBlockchain(config *Config) (*Blockchain, error) {
	logger.Infof("Initializing custom blockchain with ChainID: %d", config.ChainID)
	db, err := database.NewLevelDB(config.DataDir + "/chaindata")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	initialRoot := [32]byte{}
	lastStateRootBytes, _ := db.Get([]byte("lastStateRoot"))
	if lastStateRootBytes != nil {
		copy(initialRoot[:], lastStateRootBytes)
		logger.Infof("Loaded last state root from DB: %x", initialRoot)
	}

	stateDB, err := state.NewStateDB(initialRoot, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create state database: %v", err)
	}

	bc := &Blockchain{
		config:          config,
		db:              db,
		stateDB:         stateDB,
		blocks:          make(map[[32]byte]*Block),
		blockByNumber:   make(map[uint64]*Block),
		mempool:         NewMempool(),
		validator:       NewValidator(),
		cache:           cache.NewCache(),
		shutdownCh:      make(chan struct{}),
		totalDifficulty: big.NewInt(0), // Inisialisasi TD dengan 0
	}

	if err := bc.initGenesis(); err != nil { // initGenesis akan set TD awal
		db.Close()
		return nil, fmt.Errorf("failed to initialize genesis: %v", err)
	}

	// Setelah initGenesis, muat TD terakhir dari DB jika ada dan jika bukan chain baru
	// Jika initGenesis membuat genesis baru, TD sudah di-set ke difficulty genesis.
	// Jika initGenesis memuat genesis yang sudah ada, TD juga sudah di-set.
	// Namun, jika kita ingin memuat TD yang terakumulasi dari sesi sebelumnya:
	if bc.currentBlock != nil && bc.currentBlock.Header.GetNumber() > 0 { // Jika bukan chain yang baru dibuat
		tdBytes, _ := db.Get([]byte("lastTotalDifficulty"))
		if tdBytes != nil {
			bc.totalDifficulty.SetBytes(tdBytes)
			logger.Infof("Loaded last total difficulty from DB: %s", bc.totalDifficulty.String())
		} else {
			// Jika tidak ada TD tersimpan, perlu dihitung ulang (mahal) atau ada kesalahan
			logger.Warningf("Last total difficulty not found in DB. TD might be incorrect if this is not a new chain.")
			// Untuk chain yang sudah ada, idealnya hitung ulang atau error.
			// Untuk sekarang, kita biarkan TD dari blok terakhir yang dimuat (jika ada) atau dari genesis.
			// Jika currentBlock dimuat dari DB, TD harusnya juga dimuat atau dihitung ulang.
			// Untuk simplifikasi, kita asumsikan initGenesis menangani TD awal dengan benar.
		}
	}

	logger.Info("Custom blockchain initialized successfully")
	return bc, nil
}

func (bc *Blockchain) SetVirtualMachine(vm interfaces.VirtualMachine) {
	bc.vm = vm
}

func (bc *Blockchain) SetConsensus(consensus interfaces.Engine) {
	bc.consensus = consensus
}

func (bc *Blockchain) GetConsensusEngine() interfaces.Engine {
	return bc.consensus
}

// GetTotalDifficulty mengembalikan total difficulty yang tersimpan.
func (bc *Blockchain) GetTotalDifficulty() *big.Int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if bc.totalDifficulty == nil {
		return big.NewInt(0) // Fallback jika belum diinisialisasi
	}
	// Kembalikan salinan agar tidak bisa dimodifikasi dari luar
	return new(big.Int).Set(bc.totalDifficulty)
}

func (bc *Blockchain) initGenesis() error {
	logger.Info("Initializing genesis block")
	genesisHashBytes, _ := bc.db.Get([]byte("genesisBlockHash"))
	if genesisHashBytes != nil {
		var genesisHash [32]byte
		copy(genesisHash[:], genesisHashBytes)
		blockData, _ := bc.db.Get(genesisHash[:])
		if blockData != nil {
			var genesisBlock Block
			if err := json.Unmarshal(blockData, &genesisBlock); err == nil {
				bc.currentBlock = &genesisBlock
				bc.blocks[genesisHash] = &genesisBlock
				bc.blockByNumber[0] = &genesisBlock
				loadedGenesisStateDB, err := state.NewStateDB(genesisBlock.Header.StateRoot, bc.db)
				if err != nil {
					return fmt.Errorf("failed to load stateDB for stored genesis: %v", err)
				}
				bc.stateDB = loadedGenesisStateDB

				// Muat atau set TD dari genesis
				tdBytes, _ := bc.db.Get([]byte("lastTotalDifficulty")) // Coba muat TD yang tersimpan
				if tdBytes != nil && genesisBlock.Header.Number == 0 { // Hanya jika ini benar-benar TD dari genesis
					bc.totalDifficulty.SetBytes(tdBytes)
				} else {
					// Jika tidak ada TD tersimpan atau ini re-init, TD genesis = difficulty genesis
					bc.totalDifficulty.Set(genesisBlock.Header.GetDifficulty())
				}
				logger.Infof("Genesis block loaded from DB: %x. TD: %s", genesisHash, bc.totalDifficulty.String())
				return nil
			}
			logger.Warningf("Found genesis hash in DB but failed to unmarshal block, will reinitialize.")
		} else {
			logger.Warningf("Found genesis hash marker in DB but block data not found, will reinitialize.")
		}
	}

	genesisDifficulty := big.NewInt(1000)
	genesis := NewBlock(
		[32]byte{},
		0,
		[20]byte{},
		genesisDifficulty,
		bc.config.GetBlockGasLimit(),
		[]*Transaction{},
	)

	// 1. Konversi alamat string hex Anda ke [20]byte
	myAddressHex := "50e8f7d870f4c22e00302c94a76a5cdf5f507961" // Tanpa "0x" untuk DecodeString
	myAddressBytes, err := hex.DecodeString(myAddressHex)
	if err != nil {
		// Tangani error jika alamat tidak valid
		return fmt.Errorf("gagal decode alamat hex Anda untuk genesis: %v", err)
	}
	var myAddressForAlloc [20]byte
	copy(myAddressForAlloc[:], myAddressBytes)

	// 2. Tentukan jumlah saldo (misalnya 1500 koin)
	// 1500 * 10^18
	myBalanceStr := "15000000000000000000000000"
	myAllocatedBalance, ok := new(big.Int).SetString(myBalanceStr, 10)
	if !ok {
		return fmt.Errorf("gagal set string untuk saldo alokasi Anda")
	}
	// 3. Definisikan alokasi
	genesisAllocation := map[[20]byte]*big.Int{
		myAddressForAlloc: myAllocatedBalance,
		// Anda bisa menambahkan alamat lain jika perlu:
		// {0xAlamatLain...}: saldoLain,
	}
	for addr, balance := range genesisAllocation {
		bc.stateDB.SetBalance(addr, balance)
		bc.stateDB.SetNonce(addr, 0)
	}

	stateRoot, err := bc.stateDB.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit genesis state: %v", err)
	}
	genesis.Header.StateRoot = stateRoot
	genesis.Header.TxHash = CalculateTransactionsRoot(genesis.Transactions)
	genesis.Header.ReceiptHash = CalculateReceiptsRoot(genesis.Receipts)
	genesis.Header.Hash = genesis.CalculateHash()

	bc.blocks[genesis.Header.Hash] = genesis
	bc.blockByNumber[0] = genesis
	bc.currentBlock = genesis
	bc.totalDifficulty.Set(genesisDifficulty) // TD awal adalah difficulty genesis

	metrics.GetMetrics().IncrementBlockCount()
	logger.LogBlockEvent(0, hex.EncodeToString(genesis.Header.Hash[:]), 0, "genesis")

	if err := bc.saveBlock(genesis); err != nil {
		return fmt.Errorf("failed to save genesis block: %v", err)
	}
	if err := bc.db.Put([]byte("genesisBlockHash"), genesis.Header.Hash[:]); err != nil {
		return fmt.Errorf("failed to save genesis block hash marker: %v", err)
	}
	if err := bc.db.Put([]byte("lastStateRoot"), stateRoot[:]); err != nil {
		return fmt.Errorf("failed to save initial state root: %v", err)
	}
	// Simpan TD awal ke DB
	if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
		logger.Warningf("Failed to save initial total difficulty: %v", err)
	}

	logger.Info("Genesis block created and saved successfully")
	return nil
}

func (bc *Blockchain) GetConfig() interfaces.ChainConfigItf {
	return bc.config
}

func (bc *Blockchain) GetStateDB() *state.StateDB {
	return bc.stateDB
}

func (bc *Blockchain) GetCurrentBlock() *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock
}

func (bc *Blockchain) GetBlockByHash(hash [32]byte) *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if cachedBlock, found := bc.cache.Get(string(hash[:])); found {
		if block, ok := cachedBlock.(*Block); ok {
			return block
		}
	}
	block := bc.blocks[hash]
	if block != nil {
		bc.cache.Set(string(hash[:]), block, cache.DefaultTTL)
	}
	// TODO: Muat dari DB jika tidak ada di cache/map
	return block
}

func (bc *Blockchain) GetBlockByNumber(number uint64) *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	blockKey := fmt.Sprintf("block_num_%d", number)
	if cachedBlock, found := bc.cache.Get(blockKey); found {
		if block, ok := cachedBlock.(*Block); ok {
			return block
		}
	}
	block := bc.blockByNumber[number]
	if block != nil {
		bc.cache.Set(blockKey, block, cache.DefaultTTL)
	}
	// TODO: Muat dari DB jika tidak ada di cache/map
	return block
}

func (bc *Blockchain) AddBlock(block *Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	logger.Debugf("Attempting to add block %d to blockchain, hash: %x", block.Header.Number, block.Header.Hash)

	if bc.currentBlock == nil {
		return errors.New("cannot add block: current block is nil, blockchain may not be initialized")
	}
	if block.Header.ParentHash != bc.currentBlock.Header.Hash {
		return fmt.Errorf("parent hash mismatch: block %d parent %x, current block %x", block.Header.Number, block.Header.ParentHash, bc.currentBlock.Header.Hash)
	}
	if block.Header.Number != bc.currentBlock.Header.Number+1 {
		return fmt.Errorf("block number out of sequence: expected %d, got %d", bc.currentBlock.Header.Number+1, block.Header.Number)
	}

	if err := bc.validator.ValidateBlock(block); err != nil {
		logger.Errorf("Block validation failed for block %d: %v", block.Header.Number, err)
		return err
	}

	if bc.consensus != nil && !bc.consensus.ValidateProofOfWork(block) {
		logger.Errorf("Invalid proof of work for block %d", block.Header.Number)
		return errors.New("invalid proof of work")
	}

	blockStateDB, err := state.NewStateDB(bc.currentBlock.Header.StateRoot, bc.db)
	if err != nil {
		return fmt.Errorf("failed to create stateDB for new block %d: %v", block.Header.Number, err)
	}

	receipts, totalGasUsed, err := bc.executeBlockTransactions(block, blockStateDB)
	if err != nil {
		logger.Errorf("Block execution failed for block %d: %v", block.Header.Number, err)
		return err
	}

	finalStateRoot, err := blockStateDB.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit state for block %d: %v", block.Header.Number, err)
	}

	block.Receipts = receipts
	block.Header.GasUsed = totalGasUsed
	block.Header.StateRoot = finalStateRoot
	block.Header.TxHash = CalculateTransactionsRoot(block.Transactions)
	block.Header.ReceiptHash = CalculateReceiptsRoot(block.Receipts)
	block.Header.Hash = block.CalculateHash()

	bc.stateDB = blockStateDB // Update stateDB utama ke state baru setelah blok ini

	// Update Total Difficulty
	if bc.totalDifficulty == nil { // Seharusnya sudah diinisialisasi
		bc.totalDifficulty = big.NewInt(0)
	}
	bc.totalDifficulty.Add(bc.totalDifficulty, block.Header.GetDifficulty())

	bc.blocks[block.Header.Hash] = block
	bc.blockByNumber[block.Header.Number] = block
	bc.currentBlock = block

	metrics.GetMetrics().IncrementBlockCount()
	metrics.GetMetrics().SetTransactionPoolSize(uint32(bc.mempool.Size()))
	logger.LogBlockEvent(block.Header.Number, hex.EncodeToString(block.Header.Hash[:]), len(block.Transactions), hex.EncodeToString(block.Header.Miner[:]))

	if err := bc.saveBlock(block); err != nil {
		logger.Errorf("Failed to save block %d: %v", block.Header.Number, err)
		// Pertimbangkan rollback state jika penyimpanan gagal
		return err
	}
	if err := bc.db.Put([]byte("lastStateRoot"), finalStateRoot[:]); err != nil {
		return fmt.Errorf("failed to save state root for block %d: %v", block.Header.Number, err)
	}
	// Simpan TD baru ke DB
	if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
		logger.Warningf("Failed to save total difficulty for block %d: %v", block.Header.Number, err)
	}

	for _, tx := range block.Transactions {
		bc.mempool.RemoveTransaction(tx.Hash)
	}
	metrics.GetMetrics().SetTransactionPoolSize(uint32(bc.mempool.Size()))

	logger.Infof("Block %d (%x) added successfully. New TD: %s", block.Header.Number, block.Header.Hash, bc.totalDifficulty.String())
	return nil
}

func (bc *Blockchain) executeBlockTransactions(block *Block, blockStateDB *state.StateDB) ([]*TransactionReceipt, uint64, error) {
	var receipts []*TransactionReceipt
	cumulativeGasUsedOnBlock := big.NewInt(0)

	for i, tx := range block.Transactions {
		snapshot := blockStateDB.Snapshot()

		execCtx := &interfaces.ExecutionContext{
			Transaction: tx,
			BlockHeader: block.Header,
			StateDB:     blockStateDB,
			GasUsedPool: cumulativeGasUsedOnBlock,
		}

		if bc.vm == nil {
			return nil, 0, errors.New("virtual machine not set on blockchain")
		}

		execResult, execErr := bc.vm.ExecuteTransaction(execCtx)

		txHashVal := tx.GetHash()
		txFromVal := tx.GetFrom()

		receipt := &TransactionReceipt{
			TxHash:      txHashVal,
			TxIndex:     uint64(i),
			BlockHash:   block.Header.GetHash(), // Hash blok saat ini (akan diisi setelah semua tx)
			BlockNumber: block.Header.GetNumber(),
			From:        txFromVal,
			To:          tx.GetTo(),
		}

		if execErr != nil || (execResult != nil && execResult.Status == 0) {
			blockStateDB.RevertToSnapshot(snapshot)
			receipt.Status = 0
			if execResult != nil {
				receipt.GasUsed = execResult.GasUsed
			} else {
				receipt.GasUsed = tx.GetGasLimit()
			}
			logger.Warningf("Transaction %x in block %d failed or reverted: %v", txHashVal, block.Header.GetNumber(), execResult.Err)
			logger.LogTransactionEvent(hex.EncodeToString(txHashVal[:]), hex.EncodeToString(txFromVal[:]), "failed_tx_in_block", tx.GetValue().String(), "failed_vm_execution")
		} else if execResult != nil {
			receipt.Status = execResult.Status
			receipt.GasUsed = execResult.GasUsed
			receipt.ContractAddress = execResult.ContractAddress
			receipt.Logs = make([]*Log, len(execResult.Logs))
			for j, exLog := range execResult.Logs {
				receipt.Logs[j] = &Log{
					Address:     exLog.Address,
					Topics:      exLog.Topics,
					Data:        exLog.Data,
					BlockNumber: block.Header.GetNumber(),
					TxHash:      txHashVal,
					TxIndex:     uint64(i),
					BlockHash:   block.Header.GetHash(),
					Index:       uint64(j),
				}
			}
			currentNonce := blockStateDB.GetNonce(txFromVal)
			blockStateDB.SetNonce(txFromVal, currentNonce+1)
			logger.LogTransactionEvent(hex.EncodeToString(txHashVal[:]), hex.EncodeToString(txFromVal[:]), "executed_in_block", tx.GetValue().String(), "success")
		} else {
			blockStateDB.RevertToSnapshot(snapshot)
			receipt.Status = 0
			receipt.GasUsed = tx.GetGasLimit()
			if execCtx.GasUsedPool != nil {
				execCtx.GasUsedPool.Add(execCtx.GasUsedPool, new(big.Int).SetUint64(tx.GetGasLimit()))
			}
			logger.Errorf("Unexpected nil execResult without error for tx %x", txHashVal)
		}

		receipt.CumulativeGasUsed = cumulativeGasUsedOnBlock.Uint64()
		receipts = append(receipts, receipt)

		if cumulativeGasUsedOnBlock.Uint64() > block.Header.GetGasLimit() {
			return nil, 0, fmt.Errorf("block gas limit %d exceeded, gas used: %d", block.Header.GetGasLimit(), cumulativeGasUsedOnBlock.Uint64())
		}
	}
	return receipts, cumulativeGasUsedOnBlock.Uint64(), nil
}

func (bc *Blockchain) saveBlock(block *Block) error {
	blockData, err := block.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize block %d: %v", block.Header.Number, err)
	}
	if err := bc.db.Put(block.Header.Hash[:], blockData); err != nil {
		return fmt.Errorf("failed to save block by hash %x: %v", block.Header.Hash, err)
	}
	keyNumToHash := append([]byte("num_"), EncodeUint64(block.Header.Number)...)
	if err := bc.db.Put(keyNumToHash, block.Header.Hash[:]); err != nil {
		return fmt.Errorf("failed to save block number mapping for %d: %v", block.Header.Number, err)
	}
	if err := bc.db.Put([]byte("currentBlock"), block.Header.Hash[:]); err != nil {
		return fmt.Errorf("failed to save current block hash marker: %v", err)
	}
	bc.cache.Set(string(block.Header.Hash[:]), block, cache.DefaultTTL)
	bc.cache.Set(fmt.Sprintf("block_num_%d", block.Header.Number), block, cache.DefaultTTL)
	return nil
}

func (bc *Blockchain) AddTransaction(tx *Transaction) error {
	txHash := tx.GetHash()
	txHashStr := hex.EncodeToString(txHash[:])
	logger.Debugf("Adding transaction to mempool: %s", txHashStr)
	if err := bc.validator.ValidateTransaction(tx, false); err != nil {
		logger.Errorf("Transaction validation failed for %s: %v", txHashStr, err)
		return err
	}
	if err := bc.mempool.AddTransaction(tx); err != nil {
		logger.Errorf("Failed to add transaction %s to mempool: %v", txHashStr, err)
		return err
	}
	metrics.GetMetrics().SetTransactionPoolSize(uint32(bc.mempool.Size()))
	logger.Debugf("Transaction %s added to mempool successfully", txHashStr)
	return nil
}

func (bc *Blockchain) GetMempool() *Mempool {
	return bc.mempool
}

func (bc *Blockchain) Close() error {
	logger.Info("Closing blockchain")
	close(bc.shutdownCh)
	if err := bc.db.Close(); err != nil {
		logger.Errorf("Failed to close database: %v", err)
		return err
	}
	logger.Info("Blockchain closed successfully")
	return nil
}

func EncodeUint64(n uint64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[7-i] = byte(n >> (i * 8))
	}
	return b
}

func CalculateTransactionsRoot(transactions []*Transaction) [32]byte {
	if len(transactions) == 0 {
		return crypto.Keccak256Hash(nil)
	}
	var txHashes [][]byte
	for _, tx := range transactions {
		h := tx.GetHash()
		txHashes = append(txHashes, h[:])
	}
	var combinedHashes []byte
	for _, hashSlice := range txHashes {
		combinedHashes = append(combinedHashes, hashSlice...)
	}
	return crypto.Keccak256Hash(combinedHashes)
}

func CalculateReceiptsRoot(receipts []*TransactionReceipt) [32]byte {
	if len(receipts) == 0 {
		return crypto.Keccak256Hash(nil)
	}
	var combinedData [][]byte
	for _, r := range receipts {
		receiptBytes, err := r.ToJSON()
		if err != nil {
			logger.Errorf("Failed to serialize receipt for root calculation: %v", err)
			combinedData = append(combinedData, []byte{})
			continue
		}
		combinedData = append(combinedData, receiptBytes)
	}
	var flatData []byte
	for _, data := range combinedData {
		flatData = append(flatData, data...)
	}
	return crypto.Keccak256Hash(flatData)
}

func (bc *Blockchain) GetBalance(address [20]byte) *big.Int {
	return bc.stateDB.GetBalance(address)
}

func (bc *Blockchain) GetNonce(address [20]byte) uint64 {
	return bc.stateDB.GetNonce(address)
}

func (bc *Blockchain) GetCode(address [20]byte) []byte {
	return bc.stateDB.GetCode(address)
}

func (bc *Blockchain) GetStorageAt(address [20]byte, key [32]byte) [32]byte {
	return bc.stateDB.GetState(address, key)
}

func (bc *Blockchain) EstimateGas(tx *Transaction) (uint64, error) {
	baseGas := uint64(21000)
	if tx.GetTo() == nil {
		baseGas = 53000
	}
	if len(tx.GetData()) > 0 {
		for _, b := range tx.GetData() {
			if b == 0 {
				baseGas += 4
			} else {
				baseGas += 16
			}
		}
	}
	return baseGas, nil
}

func (bc *Blockchain) GetDatabase() database.Database {
	return bc.db
}

func (bc *Blockchain) GetBlockByHashForEVM(hash common.Hash) interfaces.BlockHeaderItf {
	var h [32]byte
	copy(h[:], hash.Bytes())
	block := bc.GetBlockByHash(h)
	if block == nil {
		return nil
	}
	return block.Header
}

func (bc *Blockchain) GetBlockByNumberForEVM(number uint64) interfaces.BlockHeaderItf {
	block := bc.GetBlockByNumber(number)
	if block == nil {
		return nil
	}
	return block.Header
}
