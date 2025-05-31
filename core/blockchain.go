package core

import (
	"blockchain-node/cache"
	appConfig "blockchain-node/config" // Menggunakan alias untuk config aplikasi
	"blockchain-node/crypto"
	"blockchain-node/database"
	"blockchain-node/evm" // Impor paket evm Anda
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"blockchain-node/metrics"
	"blockchain-node/state" // StateDB kustom
	"bytes"                 // Untuk bytes.Equal
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

	"github.com/ethereum/go-ethereum/common" // Untuk common.Address, common.Hash
	"github.com/ethereum/go-ethereum/params" // Tetap dibutuhkan untuk ToEthChainConfig
)

// Config (core.Config) mendefinisikan parameter inti blockchain.
type Config struct {
	DataDir       string
	ChainID       uint64
	BlockGasLimit uint64
}

// ToEthChainConfig mengkonversi core.Config ke params.ChainConfig milik go-ethereum.
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
		ArrowGlacierBlock:   big.NewInt(0),
		GrayGlacierBlock:    big.NewInt(0),
		MergeNetsplitBlock:  nil,
		ShanghaiTime:        nil,
		CancunTime:          nil,
	}
}

func (c *Config) GetChainID() uint64       { return c.ChainID }
func (c *Config) GetBlockGasLimit() uint64 { return c.BlockGasLimit }

// TransactionReceipt
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
	Logs              []*Log    `json:"logs"` // Menggunakan []*core.Log
	Status            uint64    `json:"status"`
	LogsBloom         []byte    `json:"logsBloom,omitempty"`
}

func (tr *TransactionReceipt) ToJSON() ([]byte, error) {
	return json.Marshal(tr)
}

// Log (struktur ini sekarang lebih dekat dengan interfaces.Log, tapi menggunakan array tetap)
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

// Blockchain struct
type Blockchain struct {
	config                  *Config
	appConfig               *appConfig.Config
	db                      database.Database
	stateDB                 *state.StateDB
	currentBlock            *Block
	blocks                  map[[32]byte]*Block
	blockByNumber           map[uint64]*Block
	mempool                 *Mempool
	vm                      interfaces.VirtualMachine
	consensus               interfaces.Engine
	validator               *Validator
	cache                   *cache.Cache // Tipe cache dari paket cache Anda
	mu                      sync.RWMutex
	shutdownCh              chan struct{}
	totalDifficulty         *big.Int
	loadedGenesisSpec       *GenesisSpec
	expectedSpecGenesisHash [32]byte
}

// Genesis struct
type GenesisAlloc struct {
	Balance string `json:"balance"`
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
		return nil, fmt.Errorf("failed to get absolute path for genesis file '%s': %w", filePath, err)
	}
	logger.Infof("Attempting to load genesis specification from: %s", absPath)

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("CRITICAL: genesis spec file not found at '%s'. This file is required for initialization", absPath)
	}
	data, err := ioutil.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis spec file '%s': %w", absPath, err)
	}
	var spec GenesisSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis spec from '%s': %w", absPath, err)
	}
	logger.Infof("Successfully loaded genesis specification: ChainID=%d, Difficulty=%s, GasLimit=%s",
		spec.Config.ChainID, spec.Difficulty, spec.GasLimit)
	return &spec, nil
}

func NewBlockchain(coreCfg *Config, nodeAppCfg *appConfig.Config) (*Blockchain, error) {
	logger.Infof("Initializing blockchain core with ChainID: %d, DataDir: %s", coreCfg.ChainID, coreCfg.DataDir)
	db, err := database.NewLevelDB(filepath.Join(coreCfg.DataDir, "chaindata"))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	bc := &Blockchain{
		config:          coreCfg,
		appConfig:       nodeAppCfg,
		db:              db,
		blocks:          make(map[[32]byte]*Block),
		blockByNumber:   make(map[uint64]*Block),
		mempool:         NewMempool(),
		validator:       NewValidator(),
		cache:           cache.NewCache(cache.DefaultTTL, cache.DefaultCleanupInterval), // Diperbaiki
		shutdownCh:      make(chan struct{}),
		totalDifficulty: big.NewInt(0),
	}

	genesisSpec, err := loadGenesisSpec(nodeAppCfg.GenesisFilePath)
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
			genesisSpec.Config.ChainID, coreCfg.ChainID, nodeAppCfg.GenesisFilePath,
		)
		logger.Error(errorMsg)
		return nil, errors.New(errorMsg)
	}

	tempGenesisBlockForHash, err := bc.calculateTheoreticalGenesisFromSpec(genesisSpec)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to calculate theoretical genesis hash from spec: %w", err)
	}
	bc.expectedSpecGenesisHash = tempGenesisBlockForHash.Header.Hash
	logger.Infof("Expected genesis hash calculated from spec: %x", bc.expectedSpecGenesisHash)

	if err := bc.initGenesis(nodeAppCfg.ForceGenesis, genesisSpec); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize genesis: %w", err)
	}

	var initialRoot [32]byte
	if bc.currentBlock != nil {
		initialRoot = bc.currentBlock.Header.StateRoot
		logger.Infof("Loading StateDB with root from current block %d: %x", bc.currentBlock.Header.Number, initialRoot)
	} else {
		logger.Info("Node starting without a committed block. Initializing StateDB with empty root, awaiting sync or genesis.")
	}

	sdb, err := state.NewStateDB(initialRoot, bc.db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create StateDB with root %x: %w", initialRoot, err)
	}
	bc.stateDB = sdb

	if !nodeAppCfg.ForceGenesis && bc.currentBlock != nil {
		if bc.currentBlock.Header.Number > 0 {
			tdBytes, _ := db.Get([]byte("lastTotalDifficulty"))
			if tdBytes != nil {
				bc.totalDifficulty.SetBytes(tdBytes)
				logger.Infof("Loaded last total difficulty from DB: %s (for existing chain, current block %d)", bc.totalDifficulty.String(), bc.currentBlock.Header.Number)
			} else {
				logger.Warningf("Last total difficulty not found in DB for existing chain. TD might be incorrect until recalculated or synced.")
				bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
			}
		} else {
			bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
			logger.Infof("Total difficulty initialized from genesis block's difficulty: %s", bc.totalDifficulty.String())
		}
	} else if bc.currentBlock != nil {
		bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
		logger.Infof("Total difficulty initialized from current (likely new/forced genesis) block's difficulty: %s", bc.totalDifficulty.String())
	}

	logger.Info("Blockchain core initialized successfully.")
	return bc, nil
}

func (bc *Blockchain) calculateTheoreticalGenesisFromSpec(spec *GenesisSpec) (*Block, error) {
	difficultyHex := strings.TrimPrefix(spec.Difficulty, "0x")
	if difficultyHex == "" {
		difficultyHex = "400"
	}
	difficulty, ok := new(big.Int).SetString(difficultyHex, 16)
	if !ok || difficulty.Sign() <= 0 {
		difficulty, _ = new(big.Int).SetString("400", 16)
		logger.Warningf("Spec: invalid or zero difficulty '%s', using default %s", spec.Difficulty, difficulty.String())
	}

	gasLimitHex := strings.TrimPrefix(spec.GasLimit, "0x")
	if gasLimitHex == "" {
		gasLimitHex = "0"
	}
	gasLimit, err := strconv.ParseUint(gasLimitHex, 16, 64)
	if err != nil || gasLimit == 0 {
		gasLimit = bc.config.GetBlockGasLimit()
		logger.Warningf("Spec: invalid or zero gasLimit '%s', using default from core.Config %d", spec.GasLimit, gasLimit)
	}

	timestampHex := strings.TrimPrefix(spec.Timestamp, "0x")
	if timestampHex == "" {
		timestampHex = "0"
	}
	timestamp, err := strconv.ParseInt(timestampHex, 16, 64)
	if err != nil {
		timestamp = 0
		logger.Warningf("Spec: invalid timestamp '%s', using default 0", spec.Timestamp)
	}

	var parentHash [32]byte
	parentHashRaw := strings.TrimPrefix(spec.ParentHash, "0x")
	if len(parentHashRaw) > 0 {
		parentHashBytes, errPH := hex.DecodeString(parentHashRaw)
		if errPH == nil && len(parentHashBytes) == 32 {
			copy(parentHash[:], parentHashBytes)
		} else {
			logger.Warningf("Spec: invalid parentHash format '%s', using zero hash. Error: %v", spec.ParentHash, errPH)
		}
	}

	var minerAddr [20]byte
	coinbaseRaw := strings.TrimPrefix(spec.Coinbase, "0x")
	if len(coinbaseRaw) > 0 {
		coinbaseBytes, errCB := hex.DecodeString(coinbaseRaw)
		if errCB == nil && len(coinbaseBytes) == 20 {
			copy(minerAddr[:], coinbaseBytes)
		} else {
			logger.Warningf("Spec: invalid coinbase format '%s', using zero address. Error: %v", spec.Coinbase, errCB)
		}
	}

	tempDB, errMemDB := database.NewMemDB() // Diperbaiki
	if errMemDB != nil {
		return nil, fmt.Errorf("spec: failed to create temporary memory database for genesis calculation: %w", errMemDB)
	}
	defer tempDB.Close()
	tempStateDB, err := state.NewStateDB([32]byte{}, tempDB)
	if err != nil {
		return nil, fmt.Errorf("spec: failed to create temporary stateDB for genesis calculation: %w", err)
	}

	for addrStr, alloc := range spec.Alloc {
		addrHex := strings.TrimPrefix(addrStr, "0x")
		if len(addrHex) != 40 {
			return nil, fmt.Errorf("spec alloc: invalid address hex length '%s'", addrStr)
		}
		addrBytes, errAllocAddr := hex.DecodeString(addrHex)
		if errAllocAddr != nil {
			return nil, fmt.Errorf("spec alloc: invalid address format '%s': %w", addrStr, errAllocAddr)
		}
		var addr [20]byte
		copy(addr[:], addrBytes)

		balanceStr := strings.TrimSpace(alloc.Balance)
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
		return nil, fmt.Errorf("spec: failed to commit temporary genesis state: %w", errStateRoot)
	}

	genesis := NewBlock(parentHash, 0, minerAddr, difficulty, gasLimit, []*Transaction{})
	genesis.Header.Timestamp = timestamp
	nonceHeaderVal, _ := strconv.ParseUint(strings.TrimPrefix(spec.Nonce, "0x"), 16, 64)
	genesis.Header.Nonce = nonceHeaderVal

	var mixHash [32]byte
	mixHashHex := strings.TrimPrefix(spec.Mixhash, "0x")
	if len(mixHashHex) > 0 {
		mixHashBytes, errMH := hex.DecodeString(mixHashHex)
		if errMH == nil && len(mixHashBytes) == 32 {
			copy(mixHash[:], mixHashBytes)
		}
	}
	genesis.Header.MixHash = mixHash

	var extraData []byte
	extraDataHex := strings.TrimPrefix(spec.ExtraData, "0x")
	if len(extraDataHex) > 0 {
		extraDataBytes, errED := hex.DecodeString(extraDataHex)
		if errED == nil {
			extraData = extraDataBytes
		}
	}
	genesis.Header.ExtraData = extraData

	genesis.Header.StateRoot = stateRoot
	genesis.Header.TxHash = CalculateTransactionsRoot(genesis.Transactions)
	genesis.Header.ReceiptHash = CalculateReceiptsRoot(nil)
	genesis.Header.Hash = genesis.CalculateHash()
	return genesis, nil
}

func (bc *Blockchain) createNewGenesisFromSpec(spec *GenesisSpec) (*Block, error) {
	logger.Warning("Creating new genesis block from specification file to be committed to main DB.")
	genesisBlock, err := bc.calculateTheoreticalGenesisFromSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("failed during theoretical calculation for new genesis: %w", err)
	}

	cleanStateDB, err := state.NewStateDB([32]byte{}, bc.db)
	if err != nil {
		return nil, fmt.Errorf("failed to create clean stateDB for new genesis: %w", err)
	}
	bc.stateDB = cleanStateDB

	for addrStr, alloc := range spec.Alloc {
		addrHex := strings.TrimPrefix(addrStr, "0x")
		addrBytes, _ := hex.DecodeString(addrHex)
		var addr [20]byte
		copy(addr[:], addrBytes)
		balance, _ := new(big.Int).SetString(alloc.Balance, 10)
		bc.stateDB.SetBalance(addr, balance)
		bc.stateDB.SetNonce(addr, 0)
		logger.Debugf("Genesis alloc (applied to main SDB): Address %s, Balance %s", addrStr, balance.String())
	}

	committedStateRoot, err := bc.stateDB.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit genesis state from spec (force): %w", err)
	}

	if !bytes.Equal(committedStateRoot[:], genesisBlock.Header.StateRoot[:]) {
		logger.Warningf("StateRoot mismatch after committing alloc for forced genesis. Theoretical: %x, Committed: %x. Using committed state root for genesis block.", genesisBlock.Header.StateRoot, committedStateRoot)
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
			return fmt.Errorf("failed to create new genesis from spec (forceGenesis): %w", err)
		}
		if bc.stateDB == nil {
			return errors.New("stateDB is nil after createNewGenesisFromSpec during forceGenesis")
		}

		bc.currentBlock = genesisBlock
		bc.blocks[genesisBlock.Header.Hash] = genesisBlock
		bc.blockByNumber[0] = genesisBlock
		bc.totalDifficulty.Set(genesisBlock.Header.GetDifficulty())
		metrics.GetMetrics().IncrementBlockCount()
		logger.LogBlockEvent(0, hex.EncodeToString(genesisBlock.Header.Hash[:]), 0, hex.EncodeToString(genesisBlock.Header.Miner[:]))

		if err := bc.saveBlock(genesisBlock); err != nil {
			return fmt.Errorf("failed to save new forced genesis block: %w", err)
		}
		if err := bc.db.Put([]byte("genesisBlockHash"), genesisBlock.Header.Hash[:]); err != nil {
			return fmt.Errorf("failed to save new forced genesis block hash marker: %w", err)
		}
		if err := bc.db.Put([]byte("lastStateRoot"), genesisBlock.Header.StateRoot[:]); err != nil {
			return fmt.Errorf("failed to save new forced initial state root: %w", err)
		}
		if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
			logger.Warningf("Failed to save new forced initial total difficulty: %v", err)
		}
		logger.Infof("New genesis block created and saved successfully from spec. Hash: %x, TD: %s, StateRoot: %x",
			genesisBlock.Header.Hash, bc.totalDifficulty.String(), genesisBlock.Header.StateRoot)
		return nil
	}

	logger.Info("ForceGenesis is false. Attempting to load existing chain data from database.")
	genesisHashMarkerBytes, errGetMarker := bc.db.Get([]byte("genesisBlockHash"))
	if errGetMarker == nil && genesisHashMarkerBytes != nil {
		var genesisHashInDB [32]byte
		copy(genesisHashInDB[:], genesisHashMarkerBytes)
		logger.Debugf("Found genesisBlockHash marker in DB: %x", genesisHashInDB)

		if !bytes.Equal(genesisHashInDB[:], bc.expectedSpecGenesisHash[:]) {
			logger.Warningf("CRITICAL MISMATCH: Genesis hash in DB (%x) does not match expected genesis hash from current spec (%x). "+
				"The datadir likely contains data for a different network or an incompatible version of this chain. "+
				"To use the current genesis.json, you might need to clear the datadir or use --forcegenesis (with caution). "+
				"Proceeding as if no valid chain data exists for this configuration.", genesisHashInDB, bc.expectedSpecGenesisHash)
			bc.currentBlock = nil
			return nil
		}

		blockData, errGetBlock := bc.db.Get(genesisHashInDB[:])
		if errGetBlock == nil && blockData != nil {
			var loadedGenesisBlock Block
			if errUnmarshal := json.Unmarshal(blockData, &loadedGenesisBlock); errUnmarshal == nil {
				if loadedGenesisBlock.Header.GetNumber() == 0 && bytes.Equal(loadedGenesisBlock.Header.Hash[:], genesisHashInDB[:]) {
					logger.Infof("Successfully unmarshalled genesis block data from DB for hash %x. Matches expected spec hash.", genesisHashInDB)
					bc.currentBlock = &loadedGenesisBlock
					bc.blocks[loadedGenesisBlock.Header.Hash] = &loadedGenesisBlock
					bc.blockByNumber[0] = &loadedGenesisBlock
					bc.cache.Set(string(loadedGenesisBlock.Header.Hash[:]), &loadedGenesisBlock, cache.DefaultTTL)
					bc.cache.Set(fmt.Sprintf("block_num_%d", 0), &loadedGenesisBlock, cache.DefaultTTL)

					currentBlockHashBytes, _ := bc.db.Get([]byte("currentBlock"))
					if currentBlockHashBytes != nil {
						var currentBlockHashDB [32]byte
						copy(currentBlockHashDB[:], currentBlockHashBytes)
						if !bytes.Equal(currentBlockHashDB[:], genesisHashInDB[:]) {
							currentBlockFromDB := bc.GetBlockByHash(currentBlockHashDB)
							if currentBlockFromDB != nil {
								logger.Infof("Loaded current block %d (%x) from DB.", currentBlockFromDB.Header.Number, currentBlockFromDB.Header.Hash)
								bc.currentBlock = currentBlockFromDB
							} else {
								logger.Warningf("currentBlock marker %x found in DB, but block data missing. Falling back to genesis.", currentBlockHashDB)
							}
						}
					}

					tdBytes, _ := bc.db.Get([]byte("lastTotalDifficulty"))
					if tdBytes != nil {
						bc.totalDifficulty.SetBytes(tdBytes)
					} else {
						bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
						logger.Warningf("Total difficulty not found in DB for existing chain, initialized from current block's difficulty: %s. Recalculation might be needed.", bc.totalDifficulty.String())
						if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
							logger.Warningf("Failed to save re-initialized total difficulty: %v", err)
						}
					}
					logger.Infof("Existing chain data loaded. Current block: %d (%x), TD: %s, Genesis: %x",
						bc.currentBlock.Header.Number, bc.currentBlock.Header.Hash, bc.totalDifficulty.String(), genesisHashInDB)
					return nil
				}
				logger.Warningf("Loaded block from DB for genesis hash %x is not number 0 or hash mismatch. Data: %+v. Proceeding as if no valid chain data.", genesisHashInDB, loadedGenesisBlock.Header)
			} else {
				logger.Warningf("Found genesis block data for hash %x in DB, but failed to unmarshal: %v. Proceeding as if no valid chain data.", genesisHashInDB, errUnmarshal)
			}
		} else {
			logger.Warningf("Found genesisBlockHash marker in DB (%x), but corresponding block data not found (err: %v). Proceeding as if no valid chain data.", genesisHashInDB, errGetBlock)
		}
	} else if errGetMarker != nil && !errors.Is(errGetMarker, database.ErrNotFound) {
		logger.Errorf("Error trying to read 'genesisBlockHash' from DB: %v. Proceeding as if no chain data.", errGetMarker)
	}

	logger.Info("No valid/matching chain data found in DB and not forcing new one. Node will await P2P sync or genesis block.")
	bc.currentBlock = nil
	return nil
}

func (bc *Blockchain) GetExpectedGenesisHash() ([32]byte, error) {
	if bc.expectedSpecGenesisHash == ([32]byte{}) {
		if bc.loadedGenesisSpec != nil {
			logger.Warning("Expected genesis hash was zero, attempting to recalculate from loaded spec.")
			tempGenesis, err := bc.calculateTheoreticalGenesisFromSpec(bc.loadedGenesisSpec)
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to recalculate expected genesis hash from loaded spec: %w", err)
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
			return fmt.Errorf("cannot validate received genesis: failed to get expected genesis hash: %w", err)
		}
		if expectedHash == ([32]byte{}) {
			return errors.New("cannot validate received genesis: expected genesis hash from spec is unknown (zero)")
		}
		if !bytes.Equal(block.Header.Hash[:], expectedHash[:]) {
			return fmt.Errorf("received genesis block hash %x does not match expected genesis hash from spec %x", block.Header.Hash, expectedHash)
		}

		logger.Infof("Received block %d (hash %x) as the new genesis block for this node. Matches spec.", block.Header.Number, block.Header.Hash)

		currentSdbRoot, _ := bc.stateDB.CurrentRoot()
		if !bytes.Equal(currentSdbRoot[:], block.Header.StateRoot[:]) {
			logger.Infof("Resetting StateDB to use StateRoot from received genesis: %x (current SDB root was %x)", block.Header.StateRoot, currentSdbRoot)
			newStateDB, errState := state.NewStateDB(block.Header.StateRoot, bc.db)
			if errState != nil {
				return fmt.Errorf("failed to set stateDB to received genesis StateRoot %x: %w", block.Header.StateRoot, errState)
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

		if bc.consensus != nil {
			if !bc.consensus.ValidateProofOfWork(block) {
				return fmt.Errorf("invalid proof of work for received genesis block %d (hash %x)", block.Header.Number, block.Header.Hash)
			}
		} else {
			logger.Warning("Consensus engine not set, skipping PoW validation for received genesis.")
		}
		bc.totalDifficulty.Set(block.Header.GetDifficulty())

	} else {
		if !bytes.Equal(block.Header.ParentHash[:], bc.currentBlock.Header.Hash[:]) {
			return fmt.Errorf("parent hash mismatch: block %d parent %x, current block %x (num %d)", block.Header.Number, block.Header.ParentHash, bc.currentBlock.Header.Hash, bc.currentBlock.Header.Number)
		}
		if block.Header.Number != bc.currentBlock.Header.Number+1 {
			return fmt.Errorf("block number out of sequence: expected %d, got %d", bc.currentBlock.Header.Number+1, block.Header.Number)
		}

		if err := bc.validator.ValidateBlock(block); err != nil {
			logger.Errorf("Block basic validation failed for block %d: %v", block.Header.Number, err)
			return err
		}

		if bc.consensus != nil {
			if !bc.consensus.ValidateProofOfWork(block) {
				logger.Errorf("Invalid proof of work for block %d", block.Header.Number)
				return errors.New("invalid proof of work")
			}
		} else {
			logger.Warningf("Consensus engine not set, skipping PoW validation for block %d", block.Header.Number)
		}

		blockStateDB, err := state.NewStateDB(bc.currentBlock.Header.StateRoot, bc.db)
		if err != nil {
			return fmt.Errorf("failed to create stateDB for new block %d from parent root %x: %w", block.Header.Number, bc.currentBlock.Header.StateRoot, err)
		}
		blockStateDB.ClearBlockLogs()

		receipts, totalGasUsed, errExec := bc.executeBlockTransactions(block, blockStateDB)
		if errExec != nil {
			logger.Errorf("Block execution failed for block %d: %v", block.Header.Number, errExec)
			return errExec
		}

		finalStateRoot, errCommit := blockStateDB.Commit()
		if errCommit != nil {
			return fmt.Errorf("failed to commit state for block %d: %w", block.Header.Number, errCommit)
		}

		block.Receipts = receipts
		block.Header.GasUsed = totalGasUsed
		block.Header.StateRoot = finalStateRoot
		block.Header.TxHash = CalculateTransactionsRoot(block.Transactions)
		block.Header.ReceiptHash = CalculateReceiptsRoot(block.Receipts)
		block.Header.Hash = block.CalculateHash()

		if bc.consensus != nil {
			if !bc.consensus.ValidateProofOfWork(block) {
				logger.Errorf("Invalid proof of work for block %d AFTER execution and final hash calculation. Original hash: %x, New hash: %x", block.Header.Number, block.Header.Hash, block.Header.Hash)
				return errors.New("invalid proof of work after execution")
			}
		}

		bc.stateDB = blockStateDB
		bc.totalDifficulty.Add(bc.totalDifficulty, block.Header.GetDifficulty())
	}

	bc.blocks[block.Header.Hash] = block
	bc.blockByNumber[block.Header.Number] = block
	bc.currentBlock = block
	bc.cache.Set(string(block.Header.Hash[:]), block, cache.DefaultTTL)
	bc.cache.Set(fmt.Sprintf("block_num_%d", block.Header.Number), block, cache.DefaultTTL)

	metrics.GetMetrics().IncrementBlockCount()
	logger.LogBlockEvent(block.Header.Number, hex.EncodeToString(block.Header.Hash[:]), len(block.Transactions), hex.EncodeToString(block.Header.Miner[:]))

	if err := bc.saveBlock(block); err != nil {
		logger.Errorf("Failed to save block %d: %v", block.Header.Number, err)
		return err
	}
	if err := bc.db.Put([]byte("lastStateRoot"), block.Header.StateRoot[:]); err != nil {
		return fmt.Errorf("failed to save state root for block %d: %w", block.Header.Number, err)
	}
	if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
		logger.Warningf("Failed to save total difficulty for block %d: %v", block.Header.Number, err)
	}
	if block.Header.Number == 0 {
		if err := bc.db.Put([]byte("genesisBlockHash"), block.Header.Hash[:]); err != nil {
			return fmt.Errorf("failed to save genesis block hash marker for synced genesis: %w", err)
		}
	}

	if block.Header.Number > 0 {
		for _, tx := range block.Transactions {
			if tx.GasPrice != nil && tx.GasPrice.Sign() > 0 {
				bc.mempool.RemoveTransaction(tx.Hash)
			}
		}
	}
	metrics.GetMetrics().SetTransactionPoolSize(uint32(bc.mempool.Size()))
	if bc.stateDB != nil {
		bc.stateDB.ClearBlockLogs()
	}

	logger.Infof("Block %d (%x) added successfully. New TD: %s, StateRoot: %x", block.Header.Number, block.Header.Hash, bc.totalDifficulty.String(), block.Header.StateRoot)
	return nil
}

func (bc *Blockchain) executeBlockTransactions(block *Block, blockStateDB *state.StateDB) ([]*TransactionReceipt, uint64, error) {
	if bc.vm == nil {
		return nil, 0, errors.New("virtual machine not set on blockchain")
	}

	var receipts []*TransactionReceipt
	cumulativeGasUsedOnBlock := big.NewInt(0)
	logIndexInBlock := uint64(0)

	for i, tx := range block.Transactions {
		execCtx := &interfaces.ExecutionContext{
			Transaction: tx,
			BlockHeader: block.Header,
			StateDB:     blockStateDB,
			GasUsedPool: cumulativeGasUsedOnBlock,
		}
		execResult, _ := bc.vm.ExecuteTransaction(execCtx)

		txHashVal := tx.GetHash()
		txFromVal := tx.GetFrom()

		receipt := &TransactionReceipt{
			TxHash:      txHashVal,
			TxIndex:     uint64(i),
			BlockHash:   block.Header.GetHash(),
			BlockNumber: block.Header.GetNumber(),
			From:        txFromVal,
			To:          tx.GetTo(),
		}

		if execResult == nil {
			logger.Errorf("CRITICAL: EVM returned nil ExecutionResult for tx %x in block %d", txHashVal, block.Header.Number)
			receipt.Status = 0
			receipt.GasUsed = tx.GetGasLimit()
		} else {
			receipt.Status = execResult.Status
			receipt.GasUsed = execResult.GasUsed
			receipt.ContractAddress = execResult.ContractAddress

			if len(execResult.Logs) > 0 {
				receipt.Logs = make([]*Log, 0, len(execResult.Logs)) // Inisialisasi slice dengan benar
				for _, iLog := range execResult.Logs {               // iLog adalah *interfaces.Log
					coreLog := &Log{
						Address:     [20]byte(iLog.Address),      // common.Address ke [20]byte
						Data:        common.CopyBytes(iLog.Data), // Salin data
						BlockNumber: iLog.BlockNumber,
						TxHash:      [32]byte(iLog.TxHash),    // common.Hash ke [32]byte
						TxIndex:     uint64(iLog.TxIndex),     // uint ke uint64
						BlockHash:   [32]byte(iLog.BlockHash), // common.Hash ke [32]byte
						Index:       logIndexInBlock,
						Removed:     false, // Default, akan diupdate jika tx gagal
					}
					// Konversi Topics: []common.Hash ke [][32]byte
					coreLog.Topics = make([][32]byte, len(iLog.Topics))
					for k, topicHash := range iLog.Topics {
						copy(coreLog.Topics[k][:], topicHash.Bytes())
					}
					receipt.Logs = append(receipt.Logs, coreLog)
					logIndexInBlock++
				}
			}

			if execResult.Status == 1 {
				isCoinbaseHeuristic := (i == 0 && (bytes.Equal(txFromVal[:], common.Address{}.Bytes()) || tx.GetValue().Cmp(big.NewInt(0)) > 0 && tx.GetGasPrice().Sign() == 0))
				if !isCoinbaseHeuristic && txFromVal != ([20]byte{}) {
					currentNonce := blockStateDB.GetNonce(txFromVal)
					blockStateDB.SetNonce(txFromVal, currentNonce+1)
				}
				logger.LogTransactionEvent(hex.EncodeToString(txHashVal[:]), hex.EncodeToString(txFromVal[:]), "executed_in_block", tx.GetValue().String(), "success")
			} else {
				logger.Warningf("Transaction %x in block %d failed (Status 0 by EVM). Gas used: %d. Error: %v", txHashVal, block.Header.GetNumber(), execResult.GasUsed, execResult.Err)
				for _, l := range receipt.Logs {
					l.Removed = true
				}
			}
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
	if cumulativeGasUsedOnBlock.Uint64() > block.Header.GetGasLimit() {
		return nil, 0, fmt.Errorf("cumulative gas used %d exceeds block gas limit %d", cumulativeGasUsedOnBlock.Uint64(), block.Header.GetGasLimit())
	}

	return receipts, cumulativeGasUsedOnBlock.Uint64(), nil
}

func (bc *Blockchain) saveBlock(block *Block) error {
	blockData, err := block.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize block %d: %w", block.Header.Number, err)
	}
	if err := bc.db.Put(block.Header.Hash[:], blockData); err != nil {
		return fmt.Errorf("failed to save block by hash %x: %w", block.Header.Hash, err)
	}
	keyNumToHash := append([]byte("num_"), EncodeUint64(block.Header.Number)...)
	if err := bc.db.Put(keyNumToHash, block.Header.Hash[:]); err != nil {
		return fmt.Errorf("failed to save block number mapping for %d: %w", block.Header.Number, err)
	}
	if err := bc.db.Put([]byte("currentBlock"), block.Header.Hash[:]); err != nil {
		return fmt.Errorf("failed to save current block hash marker: %w", err)
	}

	for _, receipt := range block.Receipts {
		receiptKey := append([]byte("receipt_"), receipt.TxHash[:]...)
		receiptData, errJson := receipt.ToJSON()
		if errJson != nil {
			logger.Warningf("Failed to serialize receipt for tx %x in block %d: %v. Skipping save.", receipt.TxHash, block.Header.Number, errJson)
			continue
		}
		if errDb := bc.db.Put(receiptKey, receiptData); errDb != nil {
			logger.Warningf("Failed to save receipt for tx %x in block %d: %v. Skipping.", receipt.TxHash, block.Header.Number, errDb)
		}
	}

	bc.cache.Set(string(block.Header.Hash[:]), block, cache.DefaultTTL)
	bc.cache.Set(fmt.Sprintf("block_num_%d", block.Header.Number), block, cache.DefaultTTL)
	return nil
}

func (bc *Blockchain) AddTransaction(tx *Transaction) error {
	bc.mu.RLock()
	sdb := bc.stateDB
	mpool := bc.mempool
	validator := bc.validator
	bc.mu.RUnlock()

	if sdb == nil {
		return errors.New("cannot add transaction: StateDB is not initialized")
	}
	if mpool == nil {
		return errors.New("cannot add transaction: Mempool is not initialized")
	}

	txHash := tx.GetHash()
	txHashStr := hex.EncodeToString(txHash[:])
	logger.Debugf("Attempting to add transaction to mempool: %s", txHashStr)

	if err := validator.ValidateTransaction(tx, false); err != nil {
		logger.Errorf("Transaction validation failed for %s: %v", txHashStr, err)
		return err
	}

	senderFrom := tx.GetFrom()
	senderBalance := sdb.GetBalance(senderFrom)

	cost := new(big.Int).Mul(tx.GetGasPrice(), new(big.Int).SetUint64(tx.GetGasLimit()))
	cost.Add(cost, tx.GetValue())

	if senderBalance.Cmp(cost) < 0 {
		err := fmt.Errorf("insufficient funds for gas*price + value. Balance: %s, Cost: %s, Tx: %s", senderBalance.String(), cost.String(), txHashStr)
		logger.Warning(err.Error())
		return err
	}

	expectedNonce := sdb.GetNonce(senderFrom)
	if tx.GetNonce() != expectedNonce {
		err := fmt.Errorf("invalid nonce for transaction %s. Account %x expects nonce %d, tx has nonce %d",
			txHashStr, senderFrom, expectedNonce, tx.GetNonce())
		logger.Warning(err.Error())
		return err
	}

	if err := mpool.AddTransaction(tx); err != nil {
		logger.Errorf("Failed to add transaction %s to mempool: %v", txHashStr, err)
		return err
	}

	metrics.GetMetrics().SetTransactionPoolSize(uint32(mpool.Size()))
	logger.Debugf("Transaction %s added to mempool successfully", txHashStr)
	return nil
}

func CalculateTransactionsRoot(transactions []*Transaction) [32]byte {
	if len(transactions) == 0 {
		return common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
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
		return common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	}
	var combinedData [][]byte
	for _, r := range receipts {
		receiptBytes, err := r.ToJSON()
		if err != nil {
			logger.Errorf("Failed to serialize receipt for root calculation: %v", err)
			errorHashResult := crypto.Keccak256Hash([]byte(err.Error())) // Diperbaiki
			combinedData = append(combinedData, errorHashResult[:])      // Diperbaiki
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

func (bc *Blockchain) GetConfig() interfaces.ChainConfigItf { return bc.config }
func (bc *Blockchain) GetAppConfig() *appConfig.Config      { return bc.appConfig }

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
	cacheKey := string(hash[:])
	if cachedBlock, found := bc.cache.Get(cacheKey); found {
		if block, ok := cachedBlock.(*Block); ok {
			return block
		}
	}
	bc.mu.RLock()
	blockFromMem := bc.blocks[hash]
	bc.mu.RUnlock()
	if blockFromMem != nil {
		bc.cache.Set(cacheKey, blockFromMem, cache.DefaultTTL)
		return blockFromMem
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
	if loadedBlock.Header != nil {
		bc.blockByNumber[loadedBlock.Header.Number] = &loadedBlock
	}
	bc.mu.Unlock()
	bc.cache.Set(cacheKey, &loadedBlock, cache.DefaultTTL)
	if loadedBlock.Header != nil {
		bc.cache.Set(fmt.Sprintf("block_num_%d", loadedBlock.Header.Number), &loadedBlock, cache.DefaultTTL)
	}
	return &loadedBlock
}

func (bc *Blockchain) GetBlockByNumber(number uint64) *Block {
	cacheKey := fmt.Sprintf("block_num_%d", number)
	if cachedBlock, found := bc.cache.Get(cacheKey); found {
		if block, ok := cachedBlock.(*Block); ok {
			return block
		}
	}
	bc.mu.RLock()
	blockFromMem := bc.blockByNumber[number]
	bc.mu.RUnlock()
	if blockFromMem != nil {
		bc.cache.Set(cacheKey, blockFromMem, cache.DefaultTTL)
		return blockFromMem
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

func (bc *Blockchain) GetMempool() *Mempool { return bc.mempool }

func (bc *Blockchain) Close() error {
	logger.Info("Closing blockchain...")
	// Hentikan goroutine cleanup cache jika ada
	if bc.cache != nil {
		bc.cache.StopCleanup() // Asumsi metode StopCleanup ada di cache.Cache
	}

	close(bc.shutdownCh)
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.db != nil {
		if err := bc.db.Close(); err != nil {
			logger.Errorf("Failed to close database: %v", err)
		} else {
			logger.Info("Database closed successfully.")
		}
		bc.db = nil
	}
	logger.Info("Blockchain closed.")
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
	sdb := bc.GetStateDB()
	if sdb == nil {
		logger.Warningf("GetBalance called but StateDB is nil for address %x", address)
		return big.NewInt(0)
	}
	return sdb.GetBalance(address)
}

func (bc *Blockchain) GetNonce(address [20]byte) uint64 {
	sdb := bc.GetStateDB()
	if sdb == nil {
		logger.Warningf("GetNonce called but StateDB is nil for address %x", address)
		return 0
	}
	return sdb.GetNonce(address)
}

func (bc *Blockchain) GetCode(address [20]byte) []byte {
	sdb := bc.GetStateDB()
	if sdb == nil {
		logger.Warningf("GetCode called but StateDB is nil for address %x", address)
		return nil
	}
	return sdb.GetCode(address)
}

func (bc *Blockchain) GetStorageAt(address [20]byte, key [32]byte) [32]byte {
	sdb := bc.GetStateDB()
	if sdb == nil {
		logger.Warningf("GetStorageAt called but StateDB is nil for address %x", address)
		return [32]byte{}
	}
	return sdb.GetState(address, key)
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

func (bc *Blockchain) GetDatabase() database.Database { return bc.db }

// Implementasi untuk antarmuka evm.Blockchain (dari paket evm Anda sendiri)
func (bc *Blockchain) GetBlockByHashForEVM(hash common.Hash) interfaces.BlockHeaderItf {
	block := bc.GetBlockByHash([32]byte(hash))
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

// Pemeriksaan implementasi antarmuka
var _ evm.Blockchain = (*Blockchain)(nil) // Diperbaiki: Menggunakan evm.Blockchain
// var _ interfaces.Blockchain = (*Blockchain)(nil) // Komentari jika tidak ada atau tidak relevan
