package core

import (
	"blockchain-node/cache"
	"blockchain-node/config" // Impor package config APLIKASI Anda
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
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	// "time" // Sudah diimpor via BlockHeader jika masih diperlukan

	"github.com/ethereum/go-ethereum/params" // Tetap dibutuhkan untuk ToEthChainConfig
)

// Config struct (core.Config) mendefinisikan parameter inti blockchain
// yang diteruskan saat membuat instance Blockchain.
type Config struct {
	DataDir       string
	ChainID       uint64
	BlockGasLimit uint64
	// Tambahkan field GenesisFilePath di sini jika Anda ingin core.Config juga menyimpannya,
	// meskipun saat ini kita mengambilnya dari config.Config aplikasi.
	// GenesisFilePath string
}

// ToEthChainConfig mengkonversi core.Config ke params.ChainConfig milik go-ethereum.
func (c *Config) ToEthChainConfig() *params.ChainConfig {
	// Ini adalah contoh konfigurasi, sesuaikan dengan kebutuhan fork Anda
	return &params.ChainConfig{
		ChainID:             big.NewInt(int64(c.ChainID)),
		HomesteadBlock:      big.NewInt(0), // Atur blok fork sesuai kebutuhan
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
		// Tambahkan fork lainnya jika perlu
	}
}

func (c *Config) GetChainID() uint64       { return c.ChainID }
func (c *Config) GetBlockGasLimit() uint64 { return c.BlockGasLimit }

// TransactionReceipt dan Log struct tetap sama seperti sebelumnya
type TransactionReceipt struct {
	TxHash            [32]byte  `json:"transactionHash"`
	TxIndex           uint64    `json:"transactionIndex"`
	BlockHash         [32]byte  `json:"blockHash"`
	BlockNumber       uint64    `json:"blockNumber"`
	From              [20]byte  `json:"from"`
	To                *[20]byte `json:"to"` // Pointer karena bisa nil
	GasUsed           uint64    `json:"gasUsed"`
	CumulativeGasUsed uint64    `json:"cumulativeGasUsed"`
	ContractAddress   *[20]byte `json:"contractAddress,omitempty"`
	Logs              []*Log    `json:"logs"`
	Status            uint64    `json:"status"`              // 1 untuk sukses, 0 untuk gagal
	LogsBloom         []byte    `json:"logsBloom,omitempty"` // Opsional
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
	Index       uint64     `json:"logIndex"` // Indeks log dalam blok
	Removed     bool       `json:"removed"`  // true jika log di-revert
}

// Blockchain struct
type Blockchain struct {
	config          *Config // Ini adalah core.Config
	db              database.Database
	stateDB         *state.StateDB
	currentBlock    *Block
	blocks          map[[32]byte]*Block // Cache blok berdasarkan hash
	blockByNumber   map[uint64]*Block   // Cache blok berdasarkan nomor
	mempool         *Mempool
	vm              interfaces.VirtualMachine
	consensus       interfaces.Engine
	validator       *Validator
	cache           *cache.Cache
	mu              sync.RWMutex
	shutdownCh      chan struct{}
	totalDifficulty *big.Int

	// BARU: Menyimpan spesifikasi genesis yang dimuat dan hash genesis yang diharapkan dari spec
	loadedGenesisSpec       *GenesisSpec
	expectedSpecGenesisHash [32]byte
}

// GenesisAlloc, GenesisChainConfig, dan GenesisSpec struct (BARU)
type GenesisAlloc struct {
	Balance string `json:"balance"` // Saldo sebagai string angka desimal
}
type GenesisChainConfig struct {
	ChainID             uint64 `json:"chainId"`
	HomesteadBlock      uint64 `json:"homesteadBlock"`
	ByzantiumBlock      uint64 `json:"byzantiumBlock"`
	ConstantinopleBlock uint64 `json:"constantinopleBlock"`
	PetersburgBlock     uint64 `json:"petersburgBlock"`
	IstanbulBlock       uint64 `json:"istanbulBlock"`
	BerlinBlock         uint64 `json:"berlinBlock"`
	LondonBlock         uint64 `json:"londonBlock"`
}
type GenesisSpec struct {
	Config     GenesisChainConfig      `json:"config"`
	Alloc      map[string]GenesisAlloc `json:"alloc"`
	Coinbase   string                  `json:"coinbase"`
	Difficulty string                  `json:"difficulty"`
	ExtraData  string                  `json:"extraData"`
	GasLimit   string                  `json:"gasLimit"`
	Nonce      string                  `json:"nonce"`
	Mixhash    string                  `json:"mixhash"`
	ParentHash string                  `json:"parentHash"`
	Timestamp  string                  `json:"timestamp"`
}

func loadGenesisSpec(filePath string) (*GenesisSpec, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for genesis file '%s': %v", filePath, err)
	}
	logger.Infof("Attempting to load genesis specification from: %s", absPath)

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("CRITICAL: genesis spec file not found at '%s'. This file is required for initialization", absPath)
	}
	data, err := ioutil.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis spec file '%s': %v", absPath, err)
	}
	var spec GenesisSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis spec from '%s': %v", absPath, err)
	}
	logger.Infof("Successfully loaded genesis specification: ChainID=%d, Difficulty=%s, GasLimit=%s",
		spec.Config.ChainID, spec.Difficulty, spec.GasLimit)
	return &spec, nil
}

func NewBlockchain(coreCfg *Config, nodeAppConfig *config.Config) (*Blockchain, error) {
	logger.Infof("Initializing custom blockchain with ChainID: %d from core.Config, DataDir: %s", coreCfg.ChainID, coreCfg.DataDir)
	db, err := database.NewLevelDB(filepath.Join(coreCfg.DataDir, "chaindata"))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	bc := &Blockchain{
		config:          coreCfg,
		db:              db,
		blocks:          make(map[[32]byte]*Block),
		blockByNumber:   make(map[uint64]*Block),
		mempool:         NewMempool(),
		validator:       NewValidator(),
		cache:           cache.NewCache(),
		shutdownCh:      make(chan struct{}),
		totalDifficulty: big.NewInt(0),
	}

	genesisSpec, err := loadGenesisSpec(nodeAppConfig.GenesisFilePath)
	if err != nil {
		db.Close()
		return nil, err
	}
	bc.loadedGenesisSpec = genesisSpec

	if genesisSpec.Config.ChainID != coreCfg.ChainID {
		db.Close()
		errorMsg := fmt.Sprintf(
			"ChainID mismatch: genesis.json specifies ChainID %d, but node configuration (core.Config) expects ChainID %d. "+
				"Ensure `chainid` in your main config file (e.g., config.yaml or command-line flag) matches `chainId` in `%s`.",
			genesisSpec.Config.ChainID, coreCfg.ChainID, nodeAppConfig.GenesisFilePath,
		)
		logger.Error(errorMsg)
		return nil, errors.New(errorMsg)
	}

	tempGenesisBlockForHash, err := bc.calculateTheoreticalGenesisFromSpec(genesisSpec)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to calculate theoretical genesis hash from spec: %v", err)
	}
	bc.expectedSpecGenesisHash = tempGenesisBlockForHash.Header.Hash
	logger.Infof("Expected genesis hash calculated from spec: %x", bc.expectedSpecGenesisHash)

	if err := bc.initGenesis(nodeAppConfig.ForceGenesis, genesisSpec); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize genesis: %v", err)
	}

	if bc.stateDB == nil {
		logger.Info("StateDB is nil after initGenesis (node is likely awaiting sync). Initializing an empty StateDB.")
		initialRoot := [32]byte{}
		sdb, err := state.NewStateDB(initialRoot, bc.db)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create initial empty stateDB post initGenesis: %v", err)
		}
		bc.stateDB = sdb
	}

	if !nodeAppConfig.ForceGenesis && bc.currentBlock != nil && bc.currentBlock.Header.GetNumber() > 0 {
		tdBytes, _ := db.Get([]byte("lastTotalDifficulty"))
		if tdBytes != nil {
			bc.totalDifficulty.SetBytes(tdBytes)
			logger.Infof("Loaded last total difficulty from DB: %s (for existing chain)", bc.totalDifficulty.String())
		} else {
			if bc.currentBlock != nil {
				// Ini adalah fallback kasar, idealnya TD dihitung ulang jika hilang dari DB untuk chain yang sudah berjalan.
				// Untuk sekarang, kita ambil dari difficulty blok terakhir yang diketahui.
				// Jika currentBlock adalah genesis, ini akan jadi difficulty genesis.
				// Jika > genesis, ini akan jadi difficulty blok itu saja, bukan akumulasi.
				// Logika TD yang benar akan mengakumulasi dari genesis.
				// Untuk simplifikasi, jika TD hilang, kita mulai dari difficulty blok saat ini.
				// Ini akan benar jika currentBlock adalah genesis.
				bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
				logger.Warningf("Last total difficulty not found in DB. Initialized TD from current block's difficulty: %s.", bc.totalDifficulty.String())
			}
		}
	} else if bc.currentBlock != nil && bc.totalDifficulty.Sign() == 0 {
		bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
		logger.Infof("Total difficulty initialized from current block's difficulty: %s", bc.totalDifficulty.String())
	} else if bc.currentBlock == nil && bc.totalDifficulty.Sign() == 0 {
		logger.Info("Node starting without a committed genesis block. Total difficulty is 0.")
	}

	logger.Info("Custom blockchain core initialized successfully.")
	return bc, nil
}

func (bc *Blockchain) calculateTheoreticalGenesisFromSpec(spec *GenesisSpec) (*Block, error) {
	difficultyHex := strings.TrimPrefix(spec.Difficulty, "0x")
	if difficultyHex == "" {
		difficultyHex = "400"
	}
	difficulty, ok := new(big.Int).SetString(difficultyHex, 16)
	if !ok {
		return nil, fmt.Errorf("spec: invalid difficulty '%s'", spec.Difficulty)
	}
	if difficulty.Sign() == 0 {
		difficulty.SetString("400", 16)
	}

	gasLimitHex := strings.TrimPrefix(spec.GasLimit, "0x")
	if gasLimitHex == "" {
		gasLimitHex = "0"
	}
	gasLimit, err := strconv.ParseUint(gasLimitHex, 16, 64)
	if err != nil {
		return nil, fmt.Errorf("spec: invalid gasLimit '%s': %v", spec.GasLimit, err)
	}
	if gasLimit == 0 && bc.config != nil {
		gasLimit = bc.config.GetBlockGasLimit()
	}

	timestampHex := strings.TrimPrefix(spec.Timestamp, "0x")
	if timestampHex == "" {
		timestampHex = "0"
	}
	timestamp, err := strconv.ParseInt(timestampHex, 16, 64)
	if err != nil {
		return nil, fmt.Errorf("spec: invalid timestamp '%s': %v", spec.Timestamp, err)
	}

	var parentHash [32]byte
	parentHashRaw := strings.TrimPrefix(spec.ParentHash, "0x")
	if len(parentHashRaw) > 0 {
		parentHashBytes, errPH := hex.DecodeString(parentHashRaw)
		if errPH == nil && len(parentHashBytes) == 32 {
			copy(parentHash[:], parentHashBytes)
		}
	}

	var minerAddr [20]byte
	coinbaseRaw := strings.TrimPrefix(spec.Coinbase, "0x")
	if len(coinbaseRaw) > 0 {
		coinbaseBytes, errCB := hex.DecodeString(coinbaseRaw)
		if errCB == nil && len(coinbaseBytes) == 20 {
			copy(minerAddr[:], coinbaseBytes)
		}
	}

	tempStateDB, err := state.NewStateDB([32]byte{}, bc.db)
	if err != nil {
		return nil, fmt.Errorf("spec: failed to create temporary stateDB: %v", err)
	}

	for addrStr, alloc := range spec.Alloc {
		addrHex := strings.TrimPrefix(addrStr, "0x")
		if len(addrHex) != 40 {
			return nil, fmt.Errorf("spec alloc: invalid address hex length '%s'", addrStr)
		}
		addrBytes, errAllocAddr := hex.DecodeString(addrHex)
		if errAllocAddr != nil {
			return nil, fmt.Errorf("spec alloc: invalid address format '%s': %v", addrStr, errAllocAddr)
		}
		var addr [20]byte
		copy(addr[:], addrBytes)
		balanceStr := alloc.Balance
		if balanceStr == "" {
			balanceStr = "0"
		}
		balance, okAllocBal := new(big.Int).SetString(balanceStr, 10)
		if !okAllocBal {
			return nil, fmt.Errorf("spec alloc: invalid balance for %s: '%s'", addrStr, alloc.Balance)
		}
		tempStateDB.SetBalance(addr, balance)
		tempStateDB.SetNonce(addr, 0)
	}
	stateRoot, errStateRoot := tempStateDB.Commit()
	if errStateRoot != nil {
		return nil, fmt.Errorf("spec: failed to commit temporary genesis state: %v", errStateRoot)
	}

	genesis := NewBlock(parentHash, 0, minerAddr, difficulty, gasLimit, []*Transaction{})
	genesis.Header.Timestamp = timestamp
	nonceHeaderVal, _ := strconv.ParseUint(strings.TrimPrefix(spec.Nonce, "0x"), 16, 64)
	genesis.Header.Nonce = nonceHeaderVal
	mixHashHex := strings.TrimPrefix(spec.Mixhash, "0x")
	if len(mixHashHex) > 0 {
		mixHashBytes, errMH := hex.DecodeString(mixHashHex)
		if errMH == nil && len(mixHashBytes) == 32 {
			copy(genesis.Header.MixHash[:], mixHashBytes)
		}
	}
	extraDataHex := strings.TrimPrefix(spec.ExtraData, "0x")
	if len(extraDataHex) > 0 {
		extraDataBytes, errED := hex.DecodeString(extraDataHex)
		if errED == nil {
			genesis.Header.ExtraData = extraDataBytes
		}
	}
	genesis.Header.StateRoot = stateRoot
	genesis.Header.TxHash = CalculateTransactionsRoot(genesis.Transactions)
	genesis.Header.ReceiptHash = CalculateReceiptsRoot(nil)
	genesis.Header.Hash = genesis.CalculateHash()
	return genesis, nil
}

func (bc *Blockchain) createNewGenesisFromSpec(spec *GenesisSpec) (*Block, error) {
	logger.Info("Creating new genesis block from specification file to be committed...")
	genesisBlock, err := bc.calculateTheoreticalGenesisFromSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("failed during theoretical calculation for new genesis: %v", err)
	}

	cleanStateDB, err := state.NewStateDB([32]byte{}, bc.db)
	if err != nil {
		return nil, fmt.Errorf("failed to create clean stateDB for new genesis: %v", err)
	}
	bc.stateDB = cleanStateDB

	for addrStr, alloc := range spec.Alloc {
		addrHex := strings.TrimPrefix(addrStr, "0x")
		if len(addrHex) != 40 {
			return nil, fmt.Errorf("alloc: invalid address hex length '%s'", addrStr)
		}
		addrBytes, errAllocAddr := hex.DecodeString(addrHex)
		if errAllocAddr != nil {
			return nil, fmt.Errorf("alloc: invalid address format '%s': %v", addrStr, errAllocAddr)
		}
		var addr [20]byte
		copy(addr[:], addrBytes)
		balanceStr := alloc.Balance
		if balanceStr == "" {
			balanceStr = "0"
		}
		balance, okAllocBal := new(big.Int).SetString(balanceStr, 10)
		if !okAllocBal {
			return nil, fmt.Errorf("alloc: invalid balance for %s: '%s'", addrStr, alloc.Balance)
		}
		bc.stateDB.SetBalance(addr, balance)
		bc.stateDB.SetNonce(addr, 0)
		logger.Debugf("Genesis alloc (force): Address %s, Balance %s", addrStr, balance.String())
	}

	committedStateRoot, err := bc.stateDB.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit genesis state from spec (force): %v", err)
	}

	if committedStateRoot != genesisBlock.Header.StateRoot {
		logger.Warningf("StateRoot mismatch after committing alloc for forced genesis. Theoretical: %x, Committed: %x. Using committed state root.", genesisBlock.Header.StateRoot, committedStateRoot)
		genesisBlock.Header.StateRoot = committedStateRoot
		genesisBlock.Header.Hash = genesisBlock.CalculateHash()
	}

	logger.Infof("Genesis stateRoot committed (force): %x", committedStateRoot)
	return genesisBlock, nil
}

func (bc *Blockchain) initGenesis(forceGenesis bool, spec *GenesisSpec) error {
	logger.Infof("Initializing genesis block. ForceGenesis: %t, Expected ChainID from spec: %d", forceGenesis, spec.Config.ChainID)

	if bc.config.ChainID != spec.Config.ChainID {
		return fmt.Errorf(
			"ChainID mismatch during initGenesis: bc.config.ChainID is %d, but genesis spec requires %d. "+
				"Ensure `chainid` in your main config file matches `chainId` in the genesis.json file.",
			bc.config.ChainID, spec.Config.ChainID,
		)
	}

	if forceGenesis {
		logger.Warning("ForceGenesis is true. Creating new genesis from specification file. This will effectively reset the chain if data for this ChainID already exists.")
		_ = bc.db.Delete([]byte("genesisBlockHash"))
		_ = bc.db.Delete([]byte("lastStateRoot"))
		_ = bc.db.Delete([]byte("lastTotalDifficulty"))

		genesisBlock, err := bc.createNewGenesisFromSpec(spec)
		if err != nil {
			return fmt.Errorf("failed to create new genesis from spec (forceGenesis): %v", err)
		}
		if bc.stateDB == nil {
			return errors.New("stateDB is nil after createNewGenesisFromSpec, this should not happen")
		}

		bc.currentBlock = genesisBlock
		bc.blocks[genesisBlock.Header.Hash] = genesisBlock
		bc.blockByNumber[0] = genesisBlock
		bc.totalDifficulty.Set(genesisBlock.Header.GetDifficulty())
		metrics.GetMetrics().IncrementBlockCount()
		logger.LogBlockEvent(0, hex.EncodeToString(genesisBlock.Header.Hash[:]), 0, hex.EncodeToString(genesisBlock.Header.Miner[:]))

		if err := bc.saveBlock(genesisBlock); err != nil {
			return fmt.Errorf("failed to save new forced genesis block: %v", err)
		}
		if err := bc.db.Put([]byte("genesisBlockHash"), genesisBlock.Header.Hash[:]); err != nil {
			return fmt.Errorf("failed to save new forced genesis block hash marker: %v", err)
		}
		if err := bc.db.Put([]byte("lastStateRoot"), genesisBlock.Header.StateRoot[:]); err != nil {
			return fmt.Errorf("failed to save new forced initial state root: %v", err)
		}
		if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
			logger.Warningf("Failed to save new forced initial total difficulty: %v", err)
		}
		logger.Infof("New genesis block created and saved successfully from spec. Hash: %x, TD: %s, StateRoot: %x",
			genesisBlock.Header.Hash, bc.totalDifficulty.String(), genesisBlock.Header.StateRoot)
		return nil
	}

	logger.Info("ForceGenesis is false. Attempting to load existing genesis from database.")
	genesisHashBytes, errGetHash := bc.db.Get([]byte("genesisBlockHash"))
	if errGetHash == nil && genesisHashBytes != nil {
		var genesisHashInDB [32]byte
		copy(genesisHashInDB[:], genesisHashBytes)
		logger.Debugf("Found genesisBlockHash marker in DB: %x", genesisHashInDB)

		if genesisHashInDB != bc.expectedSpecGenesisHash {
			logger.Warningf("Mismatch: Genesis hash in DB (%x) does not match expected genesis hash from spec (%x). "+
				"This datadir might be for a different network or an outdated version of this chain. "+
				"Proceeding as if no valid genesis in DB for this configuration.", genesisHashInDB, bc.expectedSpecGenesisHash)
		} else {
			blockData, errGetBlock := bc.db.Get(genesisHashInDB[:])
			if errGetBlock == nil && blockData != nil {
				var loadedGenesisBlock Block
				if errUnmarshal := json.Unmarshal(blockData, &loadedGenesisBlock); errUnmarshal == nil {
					if loadedGenesisBlock.Header.GetNumber() == 0 && loadedGenesisBlock.Header.Hash == genesisHashInDB {
						logger.Infof("Successfully unmarshalled block data from DB for hash %x. Matches expected spec hash.", genesisHashInDB)
						bc.currentBlock = &loadedGenesisBlock
						bc.blocks[loadedGenesisBlock.Header.Hash] = &loadedGenesisBlock
						bc.blockByNumber[0] = &loadedGenesisBlock
						bc.cache.Set(string(loadedGenesisBlock.Header.Hash[:]), &loadedGenesisBlock, cache.DefaultTTL)
						bc.cache.Set(fmt.Sprintf("block_num_%d", 0), &loadedGenesisBlock, cache.DefaultTTL)

						sdb, err := state.NewStateDB(loadedGenesisBlock.Header.StateRoot, bc.db)
						if err != nil {
							logger.Errorf("CRITICAL: Failed to load stateDB for stored genesis (StateRoot: %x): %v. Datadir might be corrupt.", loadedGenesisBlock.Header.StateRoot, err)
							return fmt.Errorf("failed to load stateDB for stored genesis: %v", err)
						}
						bc.stateDB = sdb

						tdBytes, _ := bc.db.Get([]byte("lastTotalDifficulty"))
						if tdBytes != nil {
							bc.totalDifficulty.SetBytes(tdBytes)
						} else {
							bc.totalDifficulty.Set(loadedGenesisBlock.Header.GetDifficulty())
							logger.Warningf("Total difficulty not found in DB for existing genesis, initialized from genesis difficulty: %s", bc.totalDifficulty.String())
							if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
								logger.Warningf("Failed to save initialized total difficulty: %v", err)
							}
						}
						logger.Infof("Existing genesis block loaded successfully from DB. Hash: %x, TD: %s, StateRoot: %x",
							loadedGenesisBlock.Header.Hash, bc.totalDifficulty.String(), loadedGenesisBlock.Header.StateRoot)
						return nil
					}
					logger.Warningf("Loaded genesis block from DB (Hash: %x, Number: %d) is not block 0 or hash mismatch with marker. Proceeding as if no valid genesis in DB.", loadedGenesisBlock.Header.Hash, loadedGenesisBlock.Header.Number)
				} else {
					logger.Warningf("Found genesis block data for hash %x in DB, but failed to unmarshal: %v. Proceeding as if no valid genesis in DB.", genesisHashInDB, errUnmarshal)
				}
			} else {
				logger.Warningf("Found genesisBlockHash marker in DB (%x), but corresponding block data not found (err: %v). Proceeding as if no valid genesis in DB.", genesisHashInDB, errGetBlock)
			}
		}
	} else if errGetHash != nil && !errors.Is(errGetHash, database.ErrNotFound) && errGetHash.Error() != "leveldb: not found" { // Ganti dengan error spesifik dari package database Anda jika ada
		logger.Errorf("Error trying to read 'genesisBlockHash' from DB: %v. Proceeding as if no genesis in DB.", errGetHash)
	}

	logger.Info("No valid/matching genesis found in DB and not forcing new one. Node will await P2P sync using parameters from genesis spec.")
	return nil
}

func (bc *Blockchain) GetExpectedGenesisHash() ([32]byte, error) {
	if bc.expectedSpecGenesisHash == ([32]byte{}) {
		if bc.loadedGenesisSpec != nil {
			logger.Warning("Expected genesis hash was zero, attempting to recalculate from loaded spec.")
			tempGenesis, err := bc.calculateTheoreticalGenesisFromSpec(bc.loadedGenesisSpec)
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to recalculate expected genesis hash from loaded spec: %v", err)
			}
			bc.expectedSpecGenesisHash = tempGenesis.Header.Hash
			if bc.expectedSpecGenesisHash == ([32]byte{}) {
				return [32]byte{}, errors.New("recalculated expected genesis hash from spec is still zero")
			}
			logger.Infof("Successfully recalculated expected genesis hash: %x", bc.expectedSpecGenesisHash)
		} else {
			return [32]byte{}, errors.New("expected genesis hash not available and genesis spec not loaded")
		}
	}
	return bc.expectedSpecGenesisHash, nil
}

func (bc *Blockchain) AddBlock(block *Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	logger.Debugf("Attempting to add block %d to blockchain, hash: %x", block.Header.Number, block.Header.Hash)

	if bc.currentBlock == nil {
		if block.Header.Number != 0 {
			return fmt.Errorf("cannot add block %d as first block: currentBlock is nil, expecting genesis (block 0)", block.Header.Number)
		}
		expectedHash, err := bc.GetExpectedGenesisHash()
		if err != nil {
			return fmt.Errorf("cannot validate received genesis: failed to get expected genesis hash: %v", err)
		}
		if expectedHash == ([32]byte{}) {
			return errors.New("cannot validate received genesis: expected genesis hash from spec is unknown (zero)")
		}
		if block.Header.Hash != expectedHash {
			return fmt.Errorf("received genesis block hash %x does not match expected genesis hash from spec %x", block.Header.Hash, expectedHash)
		}

		logger.Infof("Received block %d (hash %x) as the new genesis block for this node. Matches spec.", block.Header.Number, block.Header.Hash)

		// Perbaikan untuk `bc.stateDB.Trie().Hash()`
		currentRootHash, errRoot := bc.stateDB.CurrentRoot() // Menggunakan CurrentRoot()
		if errRoot != nil {
			logger.Warningf("Could not get current stateDB root hash: %v. Assuming it needs reset for genesis.", errRoot)
			currentRootHash = [32]byte{} // Anggap saja berbeda jika ada error
		}

		if bc.stateDB == nil || currentRootHash != block.Header.StateRoot {
			logger.Infof("Setting StateDB to use StateRoot from received genesis: %x", block.Header.StateRoot)
			newStateDB, errState := state.NewStateDB(block.Header.StateRoot, bc.db)
			if errState != nil {
				return fmt.Errorf("failed to set stateDB to received genesis StateRoot %x: %v", block.Header.StateRoot, errState)
			}
			bc.stateDB = newStateDB
		} else {
			logger.Debugf("Current StateDB root already matches received genesis StateRoot: %x", block.Header.StateRoot)
		}

		block.Header.GasUsed = 0
		if block.Header.TxHash == ([32]byte{}) {
			block.Header.TxHash = CalculateTransactionsRoot(block.Transactions)
		}
		if block.Header.ReceiptHash == ([32]byte{}) {
			block.Header.ReceiptHash = CalculateReceiptsRoot(block.Receipts)
		}
		if bc.consensus != nil && !bc.consensus.ValidateProofOfWork(block) {
			return fmt.Errorf("invalid proof of work for received genesis block %d (hash %x)", block.Header.Number, block.Header.Hash)
		}
		bc.totalDifficulty.Set(block.Header.GetDifficulty())

	} else {
		if block.Header.ParentHash != bc.currentBlock.Header.Hash {
			return fmt.Errorf("parent hash mismatch: block %d parent %x, current block %x (num %d)", block.Header.Number, block.Header.ParentHash, bc.currentBlock.Header.Hash, bc.currentBlock.Header.Number)
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

		receipts, totalGasUsed, errExec := bc.executeBlockTransactions(block, blockStateDB)
		if errExec != nil {
			logger.Errorf("Block execution failed for block %d: %v", block.Header.Number, errExec)
			return errExec
		}

		finalStateRoot, errCommit := blockStateDB.Commit()
		if errCommit != nil {
			return fmt.Errorf("failed to commit state for block %d: %v", block.Header.Number, errCommit)
		}

		block.Receipts = receipts
		block.Header.GasUsed = totalGasUsed
		block.Header.StateRoot = finalStateRoot
		block.Header.TxHash = CalculateTransactionsRoot(block.Transactions)
		block.Header.ReceiptHash = CalculateReceiptsRoot(block.Receipts)

		bc.stateDB = blockStateDB
		bc.totalDifficulty.Add(bc.totalDifficulty, block.Header.GetDifficulty())
	}

	bc.blocks[block.Header.Hash] = block
	bc.blockByNumber[block.Header.Number] = block
	bc.currentBlock = block

	metrics.GetMetrics().IncrementBlockCount()
	logger.LogBlockEvent(block.Header.Number, hex.EncodeToString(block.Header.Hash[:]), len(block.Transactions), hex.EncodeToString(block.Header.Miner[:]))

	if err := bc.saveBlock(block); err != nil {
		logger.Errorf("Failed to save block %d: %v", block.Header.Number, err)
		return err
	}
	if err := bc.db.Put([]byte("lastStateRoot"), block.Header.StateRoot[:]); err != nil {
		return fmt.Errorf("failed to save state root for block %d: %v", block.Header.Number, err)
	}
	if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
		logger.Warningf("Failed to save total difficulty for block %d: %v", block.Header.Number, err)
	}
	if block.Header.Number == 0 {
		if err := bc.db.Put([]byte("genesisBlockHash"), block.Header.Hash[:]); err != nil {
			return fmt.Errorf("failed to save genesis block hash marker for synced genesis: %v", err)
		}
	}

	if block.Header.Number > 0 {
		for _, tx := range block.Transactions {
			bc.mempool.RemoveTransaction(tx.Hash)
		}
	}
	metrics.GetMetrics().SetTransactionPoolSize(uint32(bc.mempool.Size()))

	logger.Infof("Block %d (%x) added successfully. New TD: %s, StateRoot: %x", block.Header.Number, block.Header.Hash, bc.totalDifficulty.String(), block.Header.StateRoot)
	return nil
}

func (bc *Blockchain) executeBlockTransactions(block *Block, blockStateDB *state.StateDB) ([]*TransactionReceipt, uint64, error) {
	if bc.vm == nil {
		return nil, 0, errors.New("virtual machine not set on blockchain")
	}

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
		execResult, execErr := bc.vm.ExecuteTransaction(execCtx)

		txHashVal := tx.GetHash()
		txFromVal := tx.GetFrom() // Ini adalah [20]byte

		receipt := &TransactionReceipt{
			TxHash:      txHashVal,
			TxIndex:     uint64(i),
			BlockHash:   block.Header.GetHash(),
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
				cumulativeGasUsedOnBlock.Add(cumulativeGasUsedOnBlock, new(big.Int).SetUint64(tx.GetGasLimit()))
			}
			logger.Warningf("Transaction %x in block %d failed or reverted: %v. Gas used: %d", txHashVal, block.Header.GetNumber(), execErr, receipt.GasUsed)
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
					Removed:     false,
				}
			}
			// Perbaikan untuk `tx.GetFrom()[:]`
			fromAddrForNonce := tx.GetFrom() // fromAddrForNonce adalah [20]byte
			// Hanya increment nonce jika bukan transaksi coinbase (indeks > 0 atau From bukan alamat nol)
			isCoinbaseHeuristic := i == 0 && (fromAddrForNonce == [20]byte{}) // Heuristik sederhana untuk coinbase
			if !isCoinbaseHeuristic {
				currentNonce := blockStateDB.GetNonce(fromAddrForNonce)
				blockStateDB.SetNonce(fromAddrForNonce, currentNonce+1)
			}
			// Untuk logging, kita perlu string hex dari alamat
			fromAddrHex := hex.EncodeToString(fromAddrForNonce[:])
			logger.LogTransactionEvent(hex.EncodeToString(txHashVal[:]), fromAddrHex, "executed_in_block", tx.GetValue().String(), "success")
		} else {
			blockStateDB.RevertToSnapshot(snapshot)
			receipt.Status = 0
			receipt.GasUsed = tx.GetGasLimit()
			cumulativeGasUsedOnBlock.Add(cumulativeGasUsedOnBlock, new(big.Int).SetUint64(tx.GetGasLimit()))
			logger.Errorf("Unexpected nil execResult without error for tx %x in block %d. Reverting.", txHashVal, block.Header.GetNumber())
		}

		receipt.CumulativeGasUsed = cumulativeGasUsedOnBlock.Uint64()
		receipts = append(receipts, receipt)

		if cumulativeGasUsedOnBlock.Uint64() > block.Header.GetGasLimit() {
			logger.Errorf("Block gas limit %d exceeded during execution of block %d. Gas used so far: %d. Tx index: %d, Tx hash: %x",
				block.Header.GetGasLimit(), block.Header.GetNumber(), cumulativeGasUsedOnBlock.Uint64(), i, txHashVal)
			return nil, 0, fmt.Errorf("block gas limit %d exceeded, gas used: %d at tx %d (%x)",
				block.Header.GetGasLimit(), cumulativeGasUsedOnBlock.Uint64(), i, txHashVal)
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
	logger.Debugf("Attempting to add transaction to mempool: %s", txHashStr)

	if err := bc.validator.ValidateTransaction(tx, false); err != nil {
		logger.Errorf("Transaction validation failed for %s: %v", txHashStr, err)
		return err
	}

	senderFrom := tx.GetFrom() // Tampung ke variabel
	senderBalance := bc.stateDB.GetBalance(senderFrom)
	cost := new(big.Int).Mul(tx.GetGasPrice(), new(big.Int).SetUint64(tx.GetGasLimit()))
	cost.Add(cost, tx.GetValue())

	if senderBalance.Cmp(cost) < 0 {
		err := fmt.Errorf("insufficient funds for gas*price + value. Balance: %s, Cost: %s, Tx: %s", senderBalance.String(), cost.String(), txHashStr)
		logger.Warning(err.Error())
		return err
	}

	expectedNonce := bc.stateDB.GetNonce(senderFrom)
	if tx.GetNonce() != expectedNonce {
		err := fmt.Errorf("invalid nonce for transaction %s. Account %x expects nonce %d, tx has nonce %d",
			txHashStr, senderFrom, expectedNonce, tx.GetNonce())
		logger.Warning(err.Error())
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

func CalculateTransactionsRoot(transactions []*Transaction) [32]byte {
	if len(transactions) == 0 {
		emptyHash := crypto.Keccak256Hash(nil) // Tampung ke variabel
		return emptyHash                       // Kembalikan variabel
	}
	var txHashes [][]byte
	for _, tx := range transactions {
		h := tx.GetHash()
		txHashes = append(txHashes, h[:]) // Ini aman karena h adalah array [32]byte
	}
	var combinedHashes []byte
	for _, hashSlice := range txHashes {
		combinedHashes = append(combinedHashes, hashSlice...)
	}
	return crypto.Keccak256Hash(combinedHashes)
}

func CalculateReceiptsRoot(receipts []*TransactionReceipt) [32]byte {
	if len(receipts) == 0 {
		emptyHash := crypto.Keccak256Hash(nil) // Tampung ke variabel
		return emptyHash                       // Kembalikan variabel
	}
	var combinedData [][]byte
	for _, r := range receipts {
		receiptBytes, err := r.ToJSON()
		if err != nil {
			logger.Errorf("Failed to serialize receipt for root calculation: %v", err)
			emptyReceiptHash := crypto.Keccak256Hash(nil)            // Tampung ke variabel
			combinedData = append(combinedData, emptyReceiptHash[:]) // Gunakan slice dari variabel
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

// --- Sisa fungsi getter dan utilitas ---
func (bc *Blockchain) GetConfig() interfaces.ChainConfigItf {
	return bc.config
}
func (bc *Blockchain) GetStateDB() *state.StateDB {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.stateDB
}
func (bc *Blockchain) GetCurrentBlock() *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock
}
func (bc *Blockchain) GetBlockByHash(hash [32]byte) *Block {
	bc.mu.RLock()
	if cachedBlock, found := bc.cache.Get(string(hash[:])); found {
		if block, ok := cachedBlock.(*Block); ok {
			bc.mu.RUnlock()
			return block
		}
	}
	block := bc.blocks[hash]
	bc.mu.RUnlock()

	if block != nil {
		bc.cache.Set(string(hash[:]), block, cache.DefaultTTL)
		return block
	}

	blockData, err := bc.db.Get(hash[:])
	if err != nil || blockData == nil {
		return nil
	}
	var loadedBlock Block
	if err := json.Unmarshal(blockData, &loadedBlock); err != nil {
		logger.Warningf("Failed to unmarshal block %x from DB: %v", hash, err)
		return nil
	}
	bc.mu.Lock()
	bc.blocks[hash] = &loadedBlock
	if loadedBlock.Header != nil { // Pastikan header tidak nil
		bc.blockByNumber[loadedBlock.Header.Number] = &loadedBlock
	}
	bc.mu.Unlock()
	bc.cache.Set(string(hash[:]), &loadedBlock, cache.DefaultTTL)
	if loadedBlock.Header != nil {
		bc.cache.Set(fmt.Sprintf("block_num_%d", loadedBlock.Header.Number), &loadedBlock, cache.DefaultTTL)
	}
	return &loadedBlock
}
func (bc *Blockchain) GetBlockByNumber(number uint64) *Block {
	bc.mu.RLock()
	blockKey := fmt.Sprintf("block_num_%d", number)
	if cachedBlock, found := bc.cache.Get(blockKey); found {
		if block, ok := cachedBlock.(*Block); ok {
			bc.mu.RUnlock()
			return block
		}
	}
	block := bc.blockByNumber[number]
	bc.mu.RUnlock()

	if block != nil {
		bc.cache.Set(blockKey, block, cache.DefaultTTL)
		return block
	}

	hashKey := append([]byte("num_"), EncodeUint64(number)...)
	blockHashBytes, err := bc.db.Get(hashKey)
	if err != nil || blockHashBytes == nil {
		return nil
	}
	var blockHash [32]byte
	copy(blockHash[:], blockHashBytes)
	return bc.GetBlockByHash(blockHash)
}
func (bc *Blockchain) GetMempool() *Mempool {
	return bc.mempool
}
func (bc *Blockchain) Close() error {
	logger.Info("Closing blockchain...")
	close(bc.shutdownCh)
	if err := bc.db.Close(); err != nil {
		logger.Errorf("Failed to close database: %v", err)
		return err
	}
	logger.Info("Blockchain closed successfully.")
	return nil
}
func EncodeUint64(n uint64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[7-i] = byte(n >> (i * 8))
	}
	return b
}
func (bc *Blockchain) GetBalance(address [20]byte) *big.Int {
	if bc.stateDB == nil {
		return big.NewInt(0)
	}
	return bc.stateDB.GetBalance(address)
}
func (bc *Blockchain) GetNonce(address [20]byte) uint64 {
	if bc.stateDB == nil {
		return 0
	}
	return bc.stateDB.GetNonce(address)
}
func (bc *Blockchain) GetCode(address [20]byte) []byte {
	if bc.stateDB == nil {
		return nil
	}
	return bc.stateDB.GetCode(address)
}
func (bc *Blockchain) GetStorageAt(address [20]byte, key [32]byte) [32]byte {
	if bc.stateDB == nil {
		return [32]byte{}
	}
	return bc.stateDB.GetState(address, key)
}
func (bc *Blockchain) EstimateGas(tx *Transaction) (uint64, error) {
	baseGas := uint64(21000)
	if tx.GetTo() == nil {
		baseGas = 53000
	}
	data := tx.GetData()
	if len(data) > 0 {
		for _, b := range data {
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
func (bc *Blockchain) GetBlockByHashForEVM(hash [32]byte) interfaces.BlockHeaderItf {
	block := bc.GetBlockByHash(hash)
	if block == nil || block.Header == nil {
		return nil
	}
	return block.Header
}
func (bc *Blockchain) GetBlockByNumberForEVM(number uint64) interfaces.BlockHeaderItf {
	block := bc.GetBlockByNumber(number)
	if block == nil || block.Header == nil {
		return nil
	}
	return block.Header
}
func (bc *Blockchain) SetVirtualMachine(vm interfaces.VirtualMachine) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.vm = vm
}
func (bc *Blockchain) SetConsensus(consensus interfaces.Engine) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.consensus = consensus
}
func (bc *Blockchain) GetConsensusEngine() interfaces.Engine {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.consensus
}
func (bc *Blockchain) GetTotalDifficulty() *big.Int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if bc.totalDifficulty == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(bc.totalDifficulty)
}
