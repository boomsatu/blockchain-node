package core

import (
	"blockchain-node/cache" // Pastikan path import ini benar
	appConfig "blockchain-node/config"
	"blockchain-node/crypto"
	"blockchain-node/database"
	"blockchain-node/evm" // Impor paket evm Anda
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"blockchain-node/metrics"
	"blockchain-node/state" // StateDB kustom
	"bytes"
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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// Config (core.Config) mendefinisikan parameter inti blockchain.
type Config struct {
	DataDir       string
	ChainID       uint64
	BlockGasLimit uint64
}

// ToEthChainConfig mengkonversi core.Config ke params.ChainConfig milik go-ethereum.
func (c *Config) ToEthChainConfig() *params.ChainConfig {
	// Implementasi ini harus sesuai dengan versi go-ethereum yang Anda gunakan
	// dan chain rules yang ingin Anda terapkan.
	return &params.ChainConfig{
		ChainID:             big.NewInt(int64(c.ChainID)),
		HomesteadBlock:      big.NewInt(0), // Sesuaikan jika chain Anda memiliki hard fork
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
		MergeNetsplitBlock:  nil, // Untuk jaringan PoS Ethereum
		ShanghaiTime:        nil,
		CancunTime:          nil,
		// Tambahkan atau hapus hard fork sesuai kebutuhan
	}
}

// GetChainID mengembalikan ID rantai.
func (c *Config) GetChainID() uint64 { return c.ChainID }

// GetBlockGasLimit mengembalikan batas gas per blok.
func (c *Config) GetBlockGasLimit() uint64 { return c.BlockGasLimit }

// TransactionReceipt menyimpan hasil dari eksekusi transaksi.
type TransactionReceipt struct {
	TxHash            [32]byte  `json:"transactionHash"`
	TxIndex           uint64    `json:"transactionIndex"`
	BlockHash         [32]byte  `json:"blockHash"`
	BlockNumber       uint64    `json:"blockNumber"`
	From              [20]byte  `json:"from"`
	To                *[20]byte `json:"to,omitempty"` // omitempty jika nil (pembuatan kontrak)
	GasUsed           uint64    `json:"gasUsed"`
	CumulativeGasUsed uint64    `json:"cumulativeGasUsed"`
	ContractAddress   *[20]byte `json:"contractAddress,omitempty"`
	Logs              []*Log    `json:"logs"`   // Menggunakan []*core.Log
	Status            uint64    `json:"status"` // 1 untuk sukses, 0 untuk gagal
	LogsBloom         []byte    `json:"logsBloom,omitempty"`
}

// ToJSON mengubah TransactionReceipt menjadi format JSON.
func (tr *TransactionReceipt) ToJSON() ([]byte, error) {
	return json.Marshal(tr)
}

// Log merepresentasikan sebuah event log yang dihasilkan oleh smart contract.
type Log struct {
	Address     [20]byte   `json:"address"`          // Alamat kontrak yang menghasilkan log
	Topics      [][32]byte `json:"topics"`           // Topik yang diindeks dari log
	Data        []byte     `json:"data"`             // Data log yang tidak diindeks
	BlockNumber uint64     `json:"blockNumber"`      // Nomor blok tempat log ini berada
	TxHash      [32]byte   `json:"transactionHash"`  // Hash transaksi yang menghasilkan log ini
	TxIndex     uint64     `json:"transactionIndex"` // Indeks transaksi dalam blok
	BlockHash   [32]byte   `json:"blockHash"`        // Hash blok tempat log ini berada
	Index       uint64     `json:"logIndex"`         // Indeks log dalam blok
	Removed     bool       `json:"removed"`          // True jika log ini berasal dari chain yang di-reorg
}

// Blockchain merepresentasikan state dan operasi dari blockchain.
type Blockchain struct {
	config                  *Config                   // Konfigurasi inti blockchain
	appConfig               *appConfig.Config         // Konfigurasi aplikasi node
	db                      database.Database         // Penyimpanan persisten untuk data blockchain
	stateDB                 *state.StateDB            // StateDB saat ini untuk EVM
	currentBlock            *Block                    // Blok terkini yang diketahui
	blocks                  map[[32]byte]*Block       // Cache blok berdasarkan hash
	blockByNumber           map[uint64]*Block         // Cache blok berdasarkan nomor
	mempool                 *Mempool                  // Pool transaksi yang belum diproses
	vm                      interfaces.VirtualMachine // Mesin virtual untuk eksekusi transaksi
	consensus               interfaces.Engine         // Mesin konsensus (misalnya, PoW)
	validator               *Validator                // Validator untuk blok dan transaksi
	cache                   *cache.Cache              // Cache umum untuk data blockchain
	mu                      sync.RWMutex              // Mutex untuk melindungi akses ke field Blockchain
	shutdownCh              chan struct{}             // Channel untuk memberi sinyal shutdown
	totalDifficulty         *big.Int                  // Total kesulitan kumulatif dari chain
	loadedGenesisSpec       *GenesisSpec              // Spesifikasi genesis yang dimuat dari file
	expectedSpecGenesisHash [32]byte                  // Hash genesis yang diharapkan berdasarkan spesifikasi
}

// GenesisAlloc mendefinisikan alokasi saldo awal dalam genesis block.
type GenesisAlloc struct {
	Balance string `json:"balance"`
}

// GenesisChainConfig mendefinisikan parameter konfigurasi rantai dalam genesis block.
type GenesisChainConfig struct {
	ChainID             uint64 `json:"chainId"`
	HomesteadBlock      uint64 `json:"homesteadBlock"`
	ByzantiumBlock      uint64 `json:"byzantiumBlock"` // Tambahkan hard fork lain jika perlu
	ConstantinopleBlock uint64 `json:"constantinopleBlock"`
	PetersburgBlock     uint64 `json:"petersburgBlock"`
	IstanbulBlock       uint64 `json:"istanbulBlock"`
	BerlinBlock         uint64 `json:"berlinBlock"`
	LondonBlock         uint64 `json:"londonBlock"`
}

// GenesisSpec mendefinisikan struktur lengkap dari file genesis.json.
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

// loadGenesisSpec memuat spesifikasi genesis dari file JSON.
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

// NewBlockchain membuat dan menginisialisasi instance Blockchain baru.
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
		cache:           cache.NewCache(cache.DefaultTTL, cache.DefaultCleanupInterval),
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

	// Hitung hash genesis teoritis dari spesifikasi untuk validasi nanti
	tempGenesisBlockForHash, err := bc.calculateTheoreticalGenesisFromSpec(genesisSpec)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to calculate theoretical genesis hash from spec: %w", err)
	}
	bc.expectedSpecGenesisHash = tempGenesisBlockForHash.Header.GetHash() // Gunakan GetHash()
	logger.Infof("Expected genesis hash calculated from spec: %x", bc.expectedSpecGenesisHash)

	// Inisialisasi genesis block (baik dari DB yang ada atau buat baru jika forceGenesis)
	if err := bc.initGenesis(nodeAppCfg.ForceGenesis, genesisSpec); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize genesis: %w", err)
	}

	// Setelah initGenesis, bc.currentBlock mungkin sudah di-set (jika chain ada atau forceGenesis)
	// atau masih nil (jika node baru dan menunggu sync).
	// bc.blockByNumber[0] juga akan di-set dengan genesis teoritis jika node baru.

	var initialRoot [32]byte
	if bc.currentBlock != nil { // Jika chain dimuat dari DB atau forceGenesis
		initialRoot = bc.currentBlock.Header.StateRoot
		logger.Infof("Loading StateDB with root from current block %d: %x", bc.currentBlock.Header.Number, initialRoot)
	} else {
		// Untuk node baru (currentBlock == nil), stateDB harus dimulai dengan root kosong.
		// Genesis state root dari spec digunakan untuk validasi hash blok genesis,
		// bukan untuk menginisialisasi stateDB kecuali jika forceGenesis=true.
		initialRoot = [32]byte{} // Root kosong untuk stateDB baru
		if theoreticalGenesis, ok := bc.blockByNumber[0]; ok {
			logger.Infof("Loading StateDB with empty root as currentBlock is nil. Theoretical genesis %x is known.", theoreticalGenesis.Header.GetHash())
		} else {
			logger.Infof("Loading StateDB with empty root as currentBlock is nil and no theoretical genesis in memory.")
		}
	}

	sdb, err := state.NewStateDB(initialRoot, bc.db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create StateDB with root %x: %w", initialRoot, err)
	}
	bc.stateDB = sdb

	// Inisialisasi totalDifficulty berdasarkan state setelah initGenesis
	if bc.currentBlock != nil { // Jika ada blok saat ini (dari DB atau genesis baru)
		if bc.currentBlock.Header.Number > 0 && !nodeAppCfg.ForceGenesis { // Chain yang ada
			tdBytes, _ := db.Get([]byte("lastTotalDifficulty"))
			if tdBytes != nil {
				bc.totalDifficulty.SetBytes(tdBytes)
				logger.Infof("Loaded last total difficulty from DB: %s (for existing chain, current block %d)", bc.totalDifficulty.String(), bc.currentBlock.Header.Number)
			} else {
				// Jika tidak ada di DB, coba hitung ulang dari awal (mahal) atau set dari current block
				logger.Warningf("Last total difficulty not found in DB for existing chain. TD might be incorrect until recalculated or synced. Setting from current block's difficulty.")
				// Seharusnya, jika chain ada, TD juga ada. Ini adalah fallback.
				bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty()) // Ini tidak akurat jika bukan genesis
			}
		} else { // Genesis (baru atau dari DB) atau forceGenesis
			bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
			logger.Infof("Total difficulty initialized from current (genesis/forced) block's difficulty: %s", bc.totalDifficulty.String())
		}
	} else if theoreticalGenesis, ok := bc.blockByNumber[0]; ok {
		// Node baru, belum ada currentBlock, tapi genesis teoritis diketahui.
		// TD akan diupdate saat sinkronisasi. Untuk status awal, gunakan TD genesis.
		bc.totalDifficulty.Set(theoreticalGenesis.Header.GetDifficulty())
		logger.Infof("Total difficulty initialized from theoretical genesis difficulty: %s (awaiting sync)", bc.totalDifficulty.String())
	}

	logger.Info("Blockchain core initialized successfully.")
	return bc, nil
}

// calculateTheoreticalGenesisFromSpec menghitung blok genesis teoritis dari spesifikasi.
// Ini tidak menulis ke DB utama, hanya untuk perhitungan hash atau pemuatan awal ke memori.
func (bc *Blockchain) calculateTheoreticalGenesisFromSpec(spec *GenesisSpec) (*Block, error) {
	difficultyHex := strings.TrimPrefix(spec.Difficulty, "0x")
	if difficultyHex == "" {
		difficultyHex = "400" // Default jika kosong
	}
	difficulty, ok := new(big.Int).SetString(difficultyHex, 16)
	if !ok || difficulty.Sign() <= 0 { // Pastikan difficulty positif
		originalDifficultyStr := spec.Difficulty
		difficulty, _ = new(big.Int).SetString("400", 16) // Fallback ke default yang valid
		logger.Warningf("Spec: invalid or non-positive difficulty '%s', using default %s (hex: %x)", originalDifficultyStr, difficulty.String(), difficulty)
	}

	gasLimitHex := strings.TrimPrefix(spec.GasLimit, "0x")
	if gasLimitHex == "" {
		gasLimitHex = "0" // Akan menggunakan default dari config jika 0
	}
	gasLimit, err := strconv.ParseUint(gasLimitHex, 16, 64)
	if err != nil || gasLimit == 0 { // Jika error atau 0, gunakan default dari core config
		originalGasLimitStr := spec.GasLimit
		gasLimit = bc.config.GetBlockGasLimit()
		logger.Warningf("Spec: invalid or zero gasLimit '%s', using default from core.Config %d", originalGasLimitStr, gasLimit)
	}

	timestampHex := strings.TrimPrefix(spec.Timestamp, "0x")
	if timestampHex == "" {
		timestampHex = "0"
	}
	timestamp, err := strconv.ParseInt(timestampHex, 16, 64)
	if err != nil {
		originalTimestampStr := spec.Timestamp
		timestamp = 0 // Default timestamp
		logger.Warningf("Spec: invalid timestamp '%s', using default %d. Error: %v", originalTimestampStr, timestamp, err)
	}

	var parentHash [32]byte // Defaultnya adalah hash nol
	parentHashRaw := strings.TrimPrefix(spec.ParentHash, "0x")
	if len(parentHashRaw) > 0 && parentHashRaw != "0000000000000000000000000000000000000000000000000000000000000000" {
		parentHashBytes, errPH := hex.DecodeString(parentHashRaw)
		if errPH == nil && len(parentHashBytes) == 32 {
			copy(parentHash[:], parentHashBytes)
		} else {
			logger.Warningf("Spec: invalid parentHash format '%s', using zero hash. Error: %v", spec.ParentHash, errPH)
		}
	}

	var minerAddr [20]byte // Defaultnya adalah alamat nol
	coinbaseRaw := strings.TrimPrefix(spec.Coinbase, "0x")
	if len(coinbaseRaw) > 0 {
		coinbaseBytes, errCB := hex.DecodeString(coinbaseRaw)
		if errCB == nil && len(coinbaseBytes) == 20 {
			copy(minerAddr[:], coinbaseBytes)
		} else {
			logger.Warningf("Spec: invalid coinbase format '%s', using zero address. Error: %v", spec.Coinbase, errCB)
		}
	}

	// Gunakan MemDB untuk state sementara saat menghitung state root genesis
	tempDB, errMemDB := database.NewMemDB()
	if errMemDB != nil {
		return nil, fmt.Errorf("spec: failed to create temporary memory database for genesis calculation: %w", errMemDB)
	}
	defer tempDB.Close()                                     // Meskipun MemDB.Close() mungkin no-op, ini praktik yang baik
	tempStateDB, err := state.NewStateDB([32]byte{}, tempDB) // Mulai dengan root kosong
	if err != nil {
		return nil, fmt.Errorf("spec: failed to create temporary stateDB for genesis calculation: %w", err)
	}

	// Alokasikan saldo ke akun dari spesifikasi genesis
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
		balance, okAllocBal := new(big.Int).SetString(balanceStr, 10) // Asumsi balance di spec adalah desimal
		if !okAllocBal {
			return nil, fmt.Errorf("spec alloc: invalid balance for %s: '%s'", addrStr, alloc.Balance)
		}
		tempStateDB.SetBalance(addr, balance)
		tempStateDB.SetNonce(addr, 0) // Nonce awal adalah 0
	}
	stateRoot, errStateRoot := tempStateDB.Commit() // Commit state sementara untuk mendapatkan root
	if errStateRoot != nil {
		return nil, fmt.Errorf("spec: failed to commit temporary genesis state: %w", errStateRoot)
	}

	// Buat objek Block genesis
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

	// Set root hash yang dihitung
	genesis.Header.StateRoot = stateRoot
	genesis.Header.TxHash = CalculateTransactionsRoot(genesis.Transactions) // Akan kosong jika tidak ada tx di genesis
	genesis.Header.ReceiptHash = CalculateReceiptsRoot(nil)                 // Tidak ada receipt untuk genesis
	genesis.Header.GasUsed = 0                                              // Tidak ada gas yang digunakan di genesis

	// Hitung hash final dari header genesis
	genesis.Header.Hash = genesis.CalculateHash()
	return genesis, nil
}

// createNewGenesisFromSpec membuat blok genesis baru dari spesifikasi dan menulisnya ke DB utama.
// Ini hanya dipanggil jika forceGenesis=true.
func (bc *Blockchain) createNewGenesisFromSpec(spec *GenesisSpec) (*Block, error) {
	logger.Warning("Creating new genesis block from specification file to be committed to main DB (forceGenesis=true).")

	// Hitung blok genesis teoritis (termasuk state root dari alloc)
	genesisBlock, err := bc.calculateTheoreticalGenesisFromSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("failed during theoretical calculation for new genesis: %w", err)
	}

	// Buat StateDB baru yang bersih yang akan menulis ke DB utama.
	// Root awal harus kosong karena kita akan membangun state genesis dari awal.
	cleanStateDB, err := state.NewStateDB([32]byte{}, bc.db)
	if err != nil {
		return nil, fmt.Errorf("failed to create clean stateDB for new genesis: %w", err)
	}
	bc.stateDB = cleanStateDB // Ganti stateDB blockchain dengan yang baru ini

	// Terapkan alokasi genesis ke stateDB yang baru ini
	for addrStr, alloc := range spec.Alloc {
		addrHex := strings.TrimPrefix(addrStr, "0x")
		addrBytes, _ := hex.DecodeString(addrHex) // Error sudah ditangani di calculateTheoreticalGenesisFromSpec
		var addr [20]byte
		copy(addr[:], addrBytes)
		balance, _ := new(big.Int).SetString(alloc.Balance, 10) // Error sudah ditangani
		bc.stateDB.SetBalance(addr, balance)
		bc.stateDB.SetNonce(addr, 0)
		logger.Debugf("Genesis alloc (applied to main SDB for forceGenesis): Address %s, Balance %s", addrStr, balance.String())
	}

	// Commit state genesis ini ke DB utama untuk mendapatkan state root yang sebenarnya
	committedStateRoot, err := bc.stateDB.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit genesis state from spec (force): %w", err)
	}

	// Bandingkan state root yang di-commit dengan yang dihitung secara teoritis.
	// Seharusnya sama jika tidak ada masalah.
	if !bytes.Equal(committedStateRoot[:], genesisBlock.Header.StateRoot[:]) {
		logger.Warningf("StateRoot mismatch after committing alloc for forced genesis. Theoretical: %x, Committed: %x. Using committed state root for genesis block.", genesisBlock.Header.StateRoot, committedStateRoot)
		genesisBlock.Header.StateRoot = committedStateRoot
		// Hitung ulang hash blok dengan state root yang benar-benar di-commit
		genesisBlock.Header.Hash = genesisBlock.CalculateHash()
	}

	logger.Infof("Genesis stateRoot committed (force): %x", committedStateRoot)
	return genesisBlock, nil
}

// initGenesis menginisialisasi genesis block, baik dari DB yang ada atau dari spesifikasi.
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
		// Hapus marker lama jika ada (opsional, tergantung strategi overwrite)
		_ = bc.db.Delete([]byte("genesisBlockHash"))
		_ = bc.db.Delete([]byte("lastStateRoot"))
		_ = bc.db.Delete([]byte("lastTotalDifficulty"))
		_ = bc.db.Delete([]byte("currentBlock")) // Hapus juga marker currentBlock

		genesisBlock, err := bc.createNewGenesisFromSpec(spec)
		if err != nil {
			return fmt.Errorf("failed to create new genesis from spec (forceGenesis): %w", err)
		}
		if bc.stateDB == nil { // Seharusnya sudah di-set di createNewGenesisFromSpec
			return errors.New("stateDB is nil after createNewGenesisFromSpec during forceGenesis")
		}

		bc.currentBlock = genesisBlock
		bc.blocks[genesisBlock.Header.GetHash()] = genesisBlock
		bc.blockByNumber[0] = genesisBlock
		bc.totalDifficulty.Set(genesisBlock.Header.GetDifficulty())
		metrics.GetMetrics().IncrementBlockCount() // Asumsi ini direset atau dimulai dari 0
		logger.LogBlockEvent(0, hex.EncodeToString(genesisBlock.Header.GetHash()[:]), 0, hex.EncodeToString(genesisBlock.Header.GetMiner()[:]))

		if err := bc.saveBlock(genesisBlock); err != nil { // saveBlock juga akan menyimpan currentBlock marker
			return fmt.Errorf("failed to save new forced genesis block: %w", err)
		}
		if err := bc.db.Put([]byte("genesisBlockHash"), genesisBlock.Header.GetHash()[:]); err != nil {
			return fmt.Errorf("failed to save new forced genesis block hash marker: %w", err)
		}
		// lastStateRoot sudah disimpan oleh saveBlock (melalui Commit di stateDB)
		// currentBlock marker juga sudah disimpan oleh saveBlock
		if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
			logger.Warningf("Failed to save new forced initial total difficulty: %v", err)
		}
		logger.Infof("New genesis block created and saved successfully from spec. Hash: %x, TD: %s, StateRoot: %x",
			genesisBlock.Header.GetHash(), bc.totalDifficulty.String(), genesisBlock.Header.StateRoot)
		return nil
	}

	// forceGenesis == false, coba muat dari DB
	logger.Info("ForceGenesis is false. Attempting to load existing chain data from database.")
	genesisHashMarkerBytes, errGetMarker := bc.db.Get([]byte("genesisBlockHash"))
	if errGetMarker == nil && genesisHashMarkerBytes != nil {
		var genesisHashInDB [32]byte
		copy(genesisHashInDB[:], genesisHashMarkerBytes)
		logger.Debugf("Found genesisBlockHash marker in DB: %x", genesisHashInDB)

		// Validasi hash genesis di DB dengan yang diharapkan dari spec saat ini
		if !bytes.Equal(genesisHashInDB[:], bc.expectedSpecGenesisHash[:]) {
			logger.Warningf("CRITICAL MISMATCH: Genesis hash in DB (%x) does not match expected genesis hash from current spec (%x). "+
				"The datadir likely contains data for a different network or an incompatible version of this chain. "+
				"To use the current genesis.json, you might need to clear the datadir or use --forcegenesis (with caution). "+
				"Proceeding as if no valid chain data exists for this configuration.", genesisHashInDB, bc.expectedSpecGenesisHash)
			bc.currentBlock = nil // Anggap tidak ada chain yang valid
			// Muat genesis teoritis ke memori untuk P2P handshake
			theoreticalGenesis, _ := bc.calculateTheoreticalGenesisFromSpec(spec)
			if theoreticalGenesis != nil {
				bc.blockByNumber[0] = theoreticalGenesis
				logger.Infof("Loaded theoretical genesis %x into memory for P2P handshake due to DB/spec mismatch.", theoreticalGenesis.Header.GetHash())
			}
			return nil // Jangan lanjutkan memuat chain dari DB jika genesis tidak cocok
		}

		// Hash genesis di DB cocok dengan spec, lanjutkan memuat blok genesis dari DB
		blockData, errGetBlock := bc.db.Get(genesisHashInDB[:])
		if errGetBlock == nil && blockData != nil {
			var loadedGenesisBlock Block
			if errUnmarshal := json.Unmarshal(blockData, &loadedGenesisBlock); errUnmarshal == nil {
				if loadedGenesisBlock.Header.GetNumber() == 0 && bytes.Equal(loadedGenesisBlock.Header.GetHash()[:], genesisHashInDB[:]) {
					logger.Infof("Successfully unmarshalled genesis block data from DB for hash %x. Matches expected spec hash.", genesisHashInDB)
					bc.currentBlock = &loadedGenesisBlock // Set currentBlock ke genesis dulu
					bc.blocks[loadedGenesisBlock.Header.GetHash()] = &loadedGenesisBlock
					bc.blockByNumber[0] = &loadedGenesisBlock
					bc.cache.Set(string(loadedGenesisBlock.Header.GetHash()[:]), &loadedGenesisBlock, cache.DefaultTTL)
					bc.cache.Set(fmt.Sprintf("block_num_%d", 0), &loadedGenesisBlock, cache.DefaultTTL)

					// Coba muat currentBlock yang sebenarnya dari marker "currentBlock"
					currentBlockHashBytes, _ := bc.db.Get([]byte("currentBlock"))
					if currentBlockHashBytes != nil {
						var currentBlockHashDB [32]byte
						copy(currentBlockHashDB[:], currentBlockHashBytes)
						if !bytes.Equal(currentBlockHashDB[:], genesisHashInDB[:]) { // Jika current block bukan genesis
							currentBlockFromDB := bc.GetBlockByHash(currentBlockHashDB) // GetBlockByHash akan memuat dari DB jika perlu
							if currentBlockFromDB != nil {
								logger.Infof("Loaded current block %d (%x) from DB marker.", currentBlockFromDB.Header.Number, currentBlockFromDB.Header.Hash)
								bc.currentBlock = currentBlockFromDB // Update currentBlock ke yang terakhir disimpan
							} else {
								logger.Warningf("currentBlock marker %x found in DB, but block data missing. Falling back to loaded genesis as currentBlock.", currentBlockHashDB)
								// bc.currentBlock tetap genesis
							}
						}
						// Jika currentBlock marker adalah genesis, bc.currentBlock sudah benar
					} else {
						logger.Info("No 'currentBlock' marker found in DB. Assuming loaded genesis is the current block.")
						// bc.currentBlock tetap genesis
					}

					// Muat total difficulty
					tdBytes, _ := bc.db.Get([]byte("lastTotalDifficulty"))
					if tdBytes != nil {
						bc.totalDifficulty.SetBytes(tdBytes)
					} else {
						// Jika tidak ada TD, dan currentBlock adalah genesis, set TD dari genesis
						// Jika currentBlock > 0 tapi TD tidak ada, ini masalah. Untuk sekarang, set dari current.
						bc.totalDifficulty.Set(bc.currentBlock.Header.GetDifficulty())
						logger.Warningf("Total difficulty not found in DB for existing chain, initialized from current block's (%d) difficulty: %s.", bc.currentBlock.Header.Number, bc.totalDifficulty.String())
						if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
							logger.Warningf("Failed to save re-initialized total difficulty: %v", err)
						}
					}
					logger.Infof("Existing chain data loaded. Current block: %d (%x), TD: %s, Genesis: %x",
						bc.currentBlock.Header.Number, bc.currentBlock.Header.GetHash(), bc.totalDifficulty.String(), genesisHashInDB)
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

	// Tidak ada chain yang valid di DB atau ada error saat membaca marker (selain not found)
	logger.Info("No valid/matching chain data found in DB and not forcing new one. Node will await P2P sync or genesis block.")
	bc.currentBlock = nil
	// Muat genesis teoritis ke memori untuk P2P handshake
	theoreticalGenesis, errCalc := bc.calculateTheoreticalGenesisFromSpec(spec)
	if errCalc == nil && theoreticalGenesis != nil {
		bc.blockByNumber[0] = theoreticalGenesis
		logger.Infof("Loaded theoretical genesis %x into memory for P2P handshake.", theoreticalGenesis.Header.GetHash())
	} else {
		logger.Errorf("Failed to load theoretical genesis into memory for P2P handshake: %v", errCalc)
	}
	return nil
}

// GetExpectedGenesisHash mengembalikan hash genesis yang diharapkan dari spesifikasi.
func (bc *Blockchain) GetExpectedGenesisHash() ([32]byte, error) {
	if bc.expectedSpecGenesisHash == ([32]byte{}) {
		if bc.loadedGenesisSpec != nil {
			logger.Warning("Expected genesis hash was zero, attempting to recalculate from loaded spec.")
			tempGenesis, err := bc.calculateTheoreticalGenesisFromSpec(bc.loadedGenesisSpec)
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to recalculate expected genesis hash from loaded spec: %w", err)
			}
			bc.expectedSpecGenesisHash = tempGenesis.Header.GetHash()
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

// AddBlock menambahkan blok baru ke blockchain.
func (bc *Blockchain) AddBlock(block *Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if block == nil || block.Header == nil {
		return errors.New("cannot add nil block or block with nil header")
	}
	blockHash := block.Header.GetHash() // Dapatkan hash dari header yang mungkin sudah final
	logger.Debugf("Attempting to add block %d to blockchain, hash: %x", block.Header.Number, blockHash)

	if bc.currentBlock == nil { // Kasus node baru menerima genesis dari P2P
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
		if !bytes.Equal(blockHash[:], expectedHash[:]) {
			return fmt.Errorf("received genesis block hash %x does not match expected genesis hash from spec %x", blockHash, expectedHash)
		}

		logger.Infof("Received block %d (hash %x) as the new genesis block for this node. Matches spec.", block.Header.Number, blockHash)

		// Validasi PoW untuk genesis yang diterima
		if bc.consensus != nil {
			if !bc.consensus.ValidateProofOfWork(block) { // block sudah memiliki hash finalnya
				return fmt.Errorf("invalid proof of work for received genesis block %d (hash %x)", block.Header.Number, blockHash)
			}
		} else {
			logger.Warning("Consensus engine not set, skipping PoW validation for received genesis.")
		}

		// Set stateDB ke root dari genesis yang diterima
		currentSdbRoot, _ := bc.stateDB.CurrentRoot() // Root stateDB saat ini (mungkin kosong)
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
		// Untuk genesis, GasUsed adalah 0, TxHash dan ReceiptHash juga biasanya root kosong atau dihitung dari list kosong
		block.Header.GasUsed = 0
		if block.Header.TxHash == ([32]byte{}) { // Jika belum di-set oleh pengirim
			block.Header.TxHash = CalculateTransactionsRoot(block.Transactions)
		}
		if block.Header.ReceiptHash == ([32]byte{}) { // Jika belum di-set
			block.Header.ReceiptHash = CalculateReceiptsRoot(block.Receipts)
		}
		// Hash blok seharusnya sudah final dari pengirim dan sudah divalidasi PoW-nya

		bc.totalDifficulty.Set(block.Header.GetDifficulty())

	} else { // Kasus blok reguler setelah genesis
		if !bytes.Equal(block.Header.ParentHash[:], bc.currentBlock.Header.GetHash()[:]) {
			return fmt.Errorf("parent hash mismatch: block %d parent %x, current block %x (num %d)", block.Header.Number, block.Header.ParentHash, bc.currentBlock.Header.GetHash(), bc.currentBlock.Header.Number)
		}
		if block.Header.Number != bc.currentBlock.Header.Number+1 {
			return fmt.Errorf("block number out of sequence: expected %d, got %d", bc.currentBlock.Header.Number+1, block.Header.Number)
		}

		// Validasi PoW sebelum eksekusi (hash di header blok harus sudah final)
		if bc.consensus != nil {
			if !bc.consensus.ValidateProofOfWork(block) {
				logger.Errorf("Invalid proof of work for block %d (hash %x) before execution", block.Header.Number, blockHash)
				return errors.New("invalid proof of work")
			}
		} else {
			logger.Warningf("Consensus engine not set, skipping PoW validation for block %d", block.Header.Number)
		}

		// Validasi dasar blok (ukuran, gas limit blok, dll.)
		if err := bc.validator.ValidateBlock(block); err != nil {
			logger.Errorf("Block basic validation failed for block %d: %v", block.Header.Number, err)
			return err
		}

		// Buat stateDB baru untuk blok ini, berdasarkan state root dari parent
		blockStateDB, err := state.NewStateDB(bc.currentBlock.Header.StateRoot, bc.db)
		if err != nil {
			return fmt.Errorf("failed to create stateDB for new block %d from parent root %x: %w", block.Header.Number, bc.currentBlock.Header.StateRoot, err)
		}
		blockStateDB.ClearBlockLogs() // Pastikan log bersih untuk blok ini

		// Eksekusi transaksi
		receipts, totalGasUsed, errExec := bc.executeBlockTransactions(block, blockStateDB)
		if errExec != nil {
			logger.Errorf("Block execution failed for block %d: %v", block.Header.Number, errExec)
			return errExec // Jangan tambahkan blok jika eksekusi gagal
		}

		// Commit state setelah eksekusi
		finalStateRoot, errCommit := blockStateDB.Commit()
		if errCommit != nil {
			return fmt.Errorf("failed to commit state for block %d: %w", block.Header.Number, errCommit)
		}

		// Update header blok dengan hasil eksekusi
		// PENTING: Hash blok TIDAK dihitung ulang di sini jika blok diterima dari P2P.
		// Hash yang ada di header blok yang diterima adalah yang divalidasi PoW-nya.
		// Jika ini adalah blok yang DIMINING secara lokal, maka hash akan dihitung ulang di akhir proses mining.
		// Untuk blok yang diterima, kita percaya hash yang ada setelah validasi PoW.
		block.Receipts = receipts
		block.Header.GasUsed = totalGasUsed
		block.Header.StateRoot = finalStateRoot
		block.Header.TxHash = CalculateTransactionsRoot(block.Transactions) // Hitung untuk konsistensi
		block.Header.ReceiptHash = CalculateReceiptsRoot(block.Receipts)    // Hitung untuk konsistensi

		// Jika hash blok yang diterima berbeda dari yang dihitung ulang setelah mengisi field di atas,
		// ini bisa menjadi indikasi masalah, TAPI untuk blok yang diterima dari P2P,
		// hash ASLI yang ada di header (yang sudah divalidasi PoW) adalah yang utama.
		// recalculatedHash := block.CalculateHash()
		// if !bytes.Equal(blockHash[:], recalculatedHash[:]) {
		// logger.Warningf("Hash mismatch for block %d after execution. Original: %x, Recalculated: %x. Using original hash.", block.Header.Number, blockHash, recalculatedHash)
		// }
		// block.Header.Hash = blockHash // Pastikan hash asli tetap digunakan

		bc.stateDB = blockStateDB // Ganti stateDB utama dengan stateDB blok ini
		bc.totalDifficulty.Add(bc.totalDifficulty, block.Header.GetDifficulty())
	}

	// Simpan blok dan update state blockchain
	bc.blocks[blockHash] = block
	bc.blockByNumber[block.Header.Number] = block
	bc.currentBlock = block
	bc.cache.Set(string(blockHash[:]), block, cache.DefaultTTL)
	bc.cache.Set(fmt.Sprintf("block_num_%d", block.Header.Number), block, cache.DefaultTTL)

	metrics.GetMetrics().IncrementBlockCount()
	logger.LogBlockEvent(block.Header.Number, hex.EncodeToString(blockHash[:]), len(block.Transactions), hex.EncodeToString(block.Header.GetMiner()[:]))

	if err := bc.saveBlock(block); err != nil { // saveBlock akan menyimpan hash blok dan marker currentBlock
		logger.Errorf("Failed to save block %d: %v", block.Header.Number, err)
		// Pertimbangkan untuk revert state jika penyimpanan gagal?
		return err
	}
	// lastStateRoot disimpan oleh saveBlock melalui stateDB.Commit() yang terjadi sebelumnya
	if err := bc.db.Put([]byte("lastTotalDifficulty"), bc.totalDifficulty.Bytes()); err != nil {
		logger.Warningf("Failed to save total difficulty for block %d: %v", block.Header.Number, err)
	}
	if block.Header.Number == 0 { // Jika ini adalah genesis yang diterima dari P2P
		if err := bc.db.Put([]byte("genesisBlockHash"), blockHash[:]); err != nil {
			return fmt.Errorf("failed to save genesis block hash marker for synced genesis: %w", err)
		}
	}

	// Hapus transaksi yang sudah masuk blok dari mempool
	// Hanya untuk transaksi non-coinbase (coinbase tidak ada di mempool)
	if block.Header.Number > 0 || (block.Header.Number == 0 && len(block.Transactions) > 0) { // Juga untuk genesis jika ada tx
		for i, tx := range block.Transactions {
			// Asumsi transaksi pertama adalah coinbase jika ini adalah blok yang baru di-mining.
			// Untuk blok yang diterima, kita perlu cara yang lebih baik untuk mengidentifikasi coinbase.
			// Untuk sekarang, kita anggap semua tx di blok yang diterima (selain yang mungkin coinbase) ada di mempool.
			isCoinbaseHeuristic := (i == 0 && tx.GetGasPrice().Sign() == 0 && tx.GetFrom() == ([20]byte{})) // Heuristik sederhana
			if !isCoinbaseHeuristic {
				bc.mempool.RemoveTransaction(tx.GetHash())
			}
		}
	}
	metrics.GetMetrics().SetTransactionPoolSize(uint32(bc.mempool.Size()))
	if bc.stateDB != nil { // bc.stateDB sudah menunjuk ke stateDB blok ini
		bc.stateDB.ClearBlockLogs() // Bersihkan log yang terkumpul di stateDB untuk blok ini
	}

	logger.Infof("Block %d (%x) added successfully. New TD: %s, StateRoot: %x", block.Header.Number, blockHash, bc.totalDifficulty.String(), block.Header.StateRoot)
	return nil
}

// executeBlockTransactions menjalankan semua transaksi dalam sebuah blok.
func (bc *Blockchain) executeBlockTransactions(block *Block, blockStateDB *state.StateDB) ([]*TransactionReceipt, uint64, error) {
	if bc.vm == nil {
		return nil, 0, errors.New("virtual machine not set on blockchain")
	}

	var receipts []*TransactionReceipt
	cumulativeGasUsedOnBlock := big.NewInt(0) // Gunakan big.Int untuk akumulasi gas
	logIndexInBlock := uint64(0)

	for i, tx := range block.Transactions {
		// Snapshot state sebelum eksekusi transaksi, untuk revert jika gagal
		snapshotID := blockStateDB.Snapshot()

		execCtx := &interfaces.ExecutionContext{
			Transaction: tx,
			BlockHeader: block.Header,
			StateDB:     blockStateDB,             // Gunakan stateDB spesifik blok ini
			GasUsedPool: cumulativeGasUsedOnBlock, // Teruskan pool gas yang sudah terpakai
		}
		execResult, errVM := bc.vm.ExecuteTransaction(execCtx) // Tangkap error dari VM

		txHashVal := tx.GetHash()
		txFromVal := tx.GetFrom()

		receipt := &TransactionReceipt{
			TxHash:      txHashVal,
			TxIndex:     uint64(i),
			BlockHash:   block.Header.GetHash(), // Hash blok diisi setelah semua tx dieksekusi dan header final
			BlockNumber: block.Header.GetNumber(),
			From:        txFromVal,
			To:          tx.GetTo(),
		}

		if execResult == nil { // Jika VM mengembalikan hasil nil (seharusnya tidak terjadi jika error ditangani)
			logger.Errorf("CRITICAL: EVM returned nil ExecutionResult for tx %x in block %d. Error from VM: %v", txHashVal, block.Header.Number, errVM)
			receipt.Status = 0                        // Gagal
			receipt.GasUsed = tx.GetGasLimit()        // Semua gas dianggap terpakai
			blockStateDB.RevertToSnapshot(snapshotID) // Kembalikan state
		} else {
			receipt.Status = execResult.Status
			receipt.GasUsed = execResult.GasUsed
			receipt.ContractAddress = execResult.ContractAddress

			if execResult.Status == 1 { // Transaksi sukses
				// Ambil log dari stateDB (yang sudah diisi oleh StateAdapter.AddLog)
				// Kita perlu cara untuk mengambil log spesifik untuk transaksi ini dari blockStateDB.
				// Untuk sekarang, kita asumsikan execResult.Logs sudah benar.
				if len(execResult.Logs) > 0 {
					receipt.Logs = make([]*Log, 0, len(execResult.Logs))
					for _, iLog := range execResult.Logs {
						coreLog := &Log{
							Address:     [20]byte(iLog.Address),
							Data:        common.CopyBytes(iLog.Data),
							BlockNumber: iLog.BlockNumber,
							TxHash:      [32]byte(iLog.TxHash),
							TxIndex:     uint64(iLog.TxIndex),
							BlockHash:   [32]byte(iLog.BlockHash), // Akan diisi dengan hash blok final nanti
							Index:       logIndexInBlock,
							Removed:     false,
						}
						coreLog.Topics = make([][32]byte, len(iLog.Topics))
						for k, topicHash := range iLog.Topics {
							copy(coreLog.Topics[k][:], topicHash.Bytes())
						}
						receipt.Logs = append(receipt.Logs, coreLog)
						logIndexInBlock++
					}
				}
				// Tingkatkan nonce untuk pengirim jika transaksi sukses dan bukan coinbase
				// Heuristik untuk coinbase: tx pertama DAN (from nol ATAU gasprice nol)
				isCoinbaseHeuristic := (i == 0 && (bytes.Equal(txFromVal[:], common.Address{}.Bytes()) || tx.GetGasPrice().Sign() == 0))
				if !isCoinbaseHeuristic && txFromVal != ([20]byte{}) {
					currentNonce := blockStateDB.GetNonce(txFromVal)
					blockStateDB.SetNonce(txFromVal, currentNonce+1)
				}
				logger.LogTransactionEvent(hex.EncodeToString(txHashVal[:]), hex.EncodeToString(txFromVal[:]), "executed_in_block", tx.GetValue().String(), "success")

			} else { // Transaksi gagal (Status 0 dari EVM atau error lain)
				logger.Warningf("Transaction %x in block %d failed (Status %d by EVM). Gas used: %d. Error: %v. Reverting state for this tx.", txHashVal, block.Header.Number, execResult.Status, execResult.GasUsed, execResult.Err)
				blockStateDB.RevertToSnapshot(snapshotID) // Kembalikan state ke sebelum tx ini
				// Log yang mungkin sudah ditambahkan oleh EVM sebelum gagal harus ditandai 'Removed'
				// atau tidak dimasukkan ke receipt. Karena kita revert, log tidak akan ada di stateDB.
				receipt.Logs = []*Log{} // Kosongkan log untuk tx gagal
			}
		}
		receipt.CumulativeGasUsed = cumulativeGasUsedOnBlock.Uint64() // Gas kumulatif SEBELUM tx ini
		receipts = append(receipts, receipt)

		// Periksa apakah total gas yang digunakan melebihi batas blok
		if cumulativeGasUsedOnBlock.Uint64() > block.Header.GetGasLimit() {
			logger.Errorf("Block gas limit %d exceeded during execution of block %d. Gas used so far: %d. Tx index: %d, Tx hash: %x",
				block.Header.GetGasLimit(), block.Header.GetNumber(), cumulativeGasUsedOnBlock.Uint64(), i, txHashVal)
			// Ini seharusnya tidak terjadi jika EVM.ExecuteTransaction mengembalikan GasUsed dengan benar
			// dan GasUsedPool (cumulativeGasUsedOnBlock) diupdate dengan benar.
			// Jika terjadi, ini adalah error kritis dalam logika gas.
			return nil, 0, fmt.Errorf("block gas limit %d exceeded, gas used: %d at tx %d (%x)",
				block.Header.GetGasLimit(), cumulativeGasUsedOnBlock.Uint64(), i, txHashVal)
		}
	}

	// Pastikan total gas yang digunakan tidak melebihi batas gas blok
	if cumulativeGasUsedOnBlock.Uint64() > block.Header.GetGasLimit() {
		return nil, 0, fmt.Errorf("cumulative gas used %d exceeds block gas limit %d after all transactions", cumulativeGasUsedOnBlock.Uint64(), block.Header.GetGasLimit())
	}

	return receipts, cumulativeGasUsedOnBlock.Uint64(), nil
}

// saveBlock menyimpan blok ke database.
func (bc *Blockchain) saveBlock(block *Block) error {
	blockData, err := block.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to serialize block %d: %w", block.Header.Number, err)
	}
	blockHash := block.Header.GetHash() // Gunakan hash yang sudah ada di header

	// Simpan blok berdasarkan hash-nya
	if err := bc.db.Put(blockHash[:], blockData); err != nil {
		return fmt.Errorf("failed to save block by hash %x: %w", blockHash, err)
	}
	// Simpan pemetaan nomor blok ke hash blok
	keyNumToHash := append([]byte("num_"), EncodeUint64(block.Header.Number)...)
	if err := bc.db.Put(keyNumToHash, blockHash[:]); err != nil {
		return fmt.Errorf("failed to save block number mapping for %d: %w", block.Header.Number, err)
	}
	// Update marker untuk blok terkini
	if err := bc.db.Put([]byte("currentBlock"), blockHash[:]); err != nil {
		return fmt.Errorf("failed to save current block hash marker: %w", err)
	}
	// Simpan state root terakhir (ini juga bisa disimpan oleh stateDB.Commit(), tergantung implementasi)
	// Jika stateDB.Commit() tidak menyimpannya secara eksplisit, simpan di sini.
	if err := bc.db.Put([]byte("lastStateRoot"), block.Header.StateRoot[:]); err != nil {
		return fmt.Errorf("failed to save state root for block %d: %w", block.Header.Number, err)
	}

	// Simpan receipts
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

	// Update cache
	bc.cache.Set(string(blockHash[:]), block, cache.DefaultTTL)
	bc.cache.Set(fmt.Sprintf("block_num_%d", block.Header.Number), block, cache.DefaultTTL)
	return nil
}

// AddTransaction menambahkan transaksi ke mempool setelah validasi.
func (bc *Blockchain) AddTransaction(tx *Transaction) error {
	bc.mu.RLock() // Gunakan RLock karena kita hanya membaca stateDB dan mempool
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

	// Validasi transaksi (bukan coinbase)
	if err := validator.ValidateTransaction(tx, false); err != nil {
		logger.Errorf("Transaction validation failed for %s: %v", txHashStr, err)
		return err
	}

	senderFrom := tx.GetFrom()
	senderBalance := sdb.GetBalance(senderFrom) // Ambil balance dari stateDB saat ini

	// Hitung biaya total transaksi
	cost := new(big.Int).Mul(tx.GetGasPrice(), new(big.Int).SetUint64(tx.GetGasLimit()))
	cost.Add(cost, tx.GetValue())

	if senderBalance.Cmp(cost) < 0 {
		err := fmt.Errorf("insufficient funds for gas*price + value. Balance: %s, Cost: %s, Tx: %s", senderBalance.String(), cost.String(), txHashStr)
		logger.Warning(err.Error())
		return err
	}

	// Validasi nonce
	expectedNonce := sdb.GetNonce(senderFrom)
	if tx.GetNonce() != expectedNonce {
		err := fmt.Errorf("invalid nonce for transaction %s. Account %x expects nonce %d, tx has nonce %d",
			txHashStr, senderFrom, expectedNonce, tx.GetNonce())
		logger.Warning(err.Error())
		return err
	}

	// Tambahkan ke mempool
	if err := mpool.AddTransaction(tx); err != nil {
		logger.Errorf("Failed to add transaction %s to mempool: %v", txHashStr, err)
		return err
	}

	metrics.GetMetrics().SetTransactionPoolSize(uint32(mpool.Size()))
	logger.Debugf("Transaction %s added to mempool successfully", txHashStr)
	return nil
}

// CalculateTransactionsRoot menghitung akar hash Merkle dari daftar transaksi.
func CalculateTransactionsRoot(transactions []*Transaction) [32]byte {
	if len(transactions) == 0 {
		// Root hash kosong standar Ethereum untuk daftar transaksi kosong
		return common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	}
	var txHashes [][]byte
	for _, tx := range transactions {
		h := tx.GetHash()
		txHashes = append(txHashes, h[:])
	}
	// Implementasi Merkle tree yang lebih baik akan ada di sini.
	// Untuk sementara, kita gabungkan hash dan hash hasilnya.
	var combinedHashes []byte
	for _, hashSlice := range txHashes {
		combinedHashes = append(combinedHashes, hashSlice...)
	}
	return crypto.Keccak256Hash(combinedHashes)
}

// CalculateReceiptsRoot menghitung akar hash Merkle dari daftar receipts.
func CalculateReceiptsRoot(receipts []*TransactionReceipt) [32]byte {
	if len(receipts) == 0 {
		// Root hash kosong standar Ethereum untuk daftar receipt kosong
		return common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	}
	var combinedData [][]byte
	for _, r := range receipts {
		receiptBytes, err := r.ToJSON() // Asumsi ToJSON menghasilkan representasi kanonikal
		if err != nil {
			logger.Errorf("Failed to serialize receipt for root calculation: %v", err)
			// Tangani error, mungkin dengan menggunakan hash dari error message
			errorHashResult := crypto.Keccak256Hash([]byte(err.Error()))
			combinedData = append(combinedData, errorHashResult[:])
			continue
		}
		combinedData = append(combinedData, receiptBytes)
	}
	// Implementasi Merkle tree yang lebih baik akan ada di sini.
	var flatData []byte
	for _, data := range combinedData {
		flatData = append(flatData, data...)
	}
	return crypto.Keccak256Hash(flatData)
}

// GetConfig mengembalikan konfigurasi inti blockchain.
func (bc *Blockchain) GetConfig() interfaces.ChainConfigItf { return bc.config }

// GetAppConfig mengembalikan konfigurasi aplikasi node.
func (bc *Blockchain) GetAppConfig() *appConfig.Config { return bc.appConfig }

// GetStateDB mengembalikan instance StateDB saat ini.
func (bc *Blockchain) GetStateDB() *state.StateDB {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.stateDB
}

// GetCurrentBlock mengembalikan blok terkini.
func (bc *Blockchain) GetCurrentBlock() *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock
}

// GetBlockByHash mengambil blok berdasarkan hash-nya, dari cache, memori, atau DB.
func (bc *Blockchain) GetBlockByHash(hash [32]byte) *Block {
	cacheKey := string(hash[:])
	if cachedBlock, found := bc.cache.Get(cacheKey); found {
		if block, ok := cachedBlock.(*Block); ok {
			return block
		}
	}
	bc.mu.RLock()
	blockFromMem, memExists := bc.blocks[hash]
	bc.mu.RUnlock()

	if memExists && blockFromMem != nil {
		bc.cache.Set(cacheKey, blockFromMem, cache.DefaultTTL)
		return blockFromMem
	}

	blockData, err := bc.db.Get(hash[:])
	if err != nil || blockData == nil { // Termasuk database.ErrNotFound
		return nil
	}
	var loadedBlock Block
	if err := json.Unmarshal(blockData, &loadedBlock); err != nil {
		logger.Warningf("Failed to unmarshal block %x from DB: %v", hash, err)
		return nil
	}

	// Simpan ke cache dan map memori untuk akses cepat berikutnya
	bc.mu.Lock()
	bc.blocks[hash] = &loadedBlock
	if loadedBlock.Header != nil { // Pastikan header tidak nil
		bc.blockByNumber[loadedBlock.Header.Number] = &loadedBlock
		bc.cache.Set(fmt.Sprintf("block_num_%d", loadedBlock.Header.Number), &loadedBlock, cache.DefaultTTL)
	}
	bc.mu.Unlock()
	bc.cache.Set(cacheKey, &loadedBlock, cache.DefaultTTL)
	return &loadedBlock
}

// GetBlockByNumber mengambil blok berdasarkan nomornya, dari cache, memori, atau DB.
func (bc *Blockchain) GetBlockByNumber(number uint64) *Block {
	cacheKey := fmt.Sprintf("block_num_%d", number)
	if cachedBlock, found := bc.cache.Get(cacheKey); found {
		if block, ok := cachedBlock.(*Block); ok {
			return block
		}
	}

	bc.mu.RLock()
	// Cek dulu di map blockByNumber yang ada di memori
	blockFromMem, memExists := bc.blockByNumber[number]
	bc.mu.RUnlock()

	if memExists && blockFromMem != nil {
		bc.cache.Set(cacheKey, blockFromMem, cache.DefaultTTL)
		return blockFromMem
	}

	// Jika tidak ada di memori, coba ambil hash dari DB berdasarkan nomor
	hashKey := append([]byte("num_"), EncodeUint64(number)...)
	blockHashBytes, err := bc.db.Get(hashKey)
	if err != nil || blockHashBytes == nil { // Termasuk database.ErrNotFound
		return nil
	}
	var blockHash [32]byte
	copy(blockHash[:], blockHashBytes)
	// Panggil GetBlockByHash untuk memuat blok (yang juga akan mengisi cache dan map memori)
	return bc.GetBlockByHash(blockHash)
}

// GetMempool mengembalikan instance mempool.
func (bc *Blockchain) GetMempool() *Mempool { return bc.mempool }

// Close menutup koneksi database dan menghentikan proses internal.
func (bc *Blockchain) Close() error {
	logger.Info("Closing blockchain...")
	if bc.cache != nil {
		bc.cache.StopCleanup()
	}

	// Kirim sinyal shutdown ke goroutine lain jika ada
	close(bc.shutdownCh)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.db != nil {
		if err := bc.db.Close(); err != nil {
			logger.Errorf("Failed to close database: %v", err)
			// Jangan return error di sini agar proses shutdown lain bisa berjalan
		} else {
			logger.Info("Database closed successfully.")
		}
		bc.db = nil
	}
	logger.Info("Blockchain closed.")
	return nil
}

// EncodeUint64 mengubah uint64 menjadi slice byte big-endian.
func EncodeUint64(n uint64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[7-i] = byte(n >> (i * 8))
	}
	return b
}

// GetBalance mengambil saldo akun dari stateDB.
func (bc *Blockchain) GetBalance(address [20]byte) *big.Int {
	sdb := bc.GetStateDB() // Sudah thread-safe
	if sdb == nil {
		logger.Warningf("GetBalance called but StateDB is nil for address %x", address)
		return big.NewInt(0)
	}
	return sdb.GetBalance(address)
}

// GetNonce mengambil nonce akun dari stateDB.
func (bc *Blockchain) GetNonce(address [20]byte) uint64 {
	sdb := bc.GetStateDB() // Sudah thread-safe
	if sdb == nil {
		logger.Warningf("GetNonce called but StateDB is nil for address %x", address)
		return 0
	}
	return sdb.GetNonce(address)
}

// GetCode mengambil kode kontrak dari stateDB.
func (bc *Blockchain) GetCode(address [20]byte) []byte {
	sdb := bc.GetStateDB() // Sudah thread-safe
	if sdb == nil {
		logger.Warningf("GetCode called but StateDB is nil for address %x", address)
		return nil
	}
	return sdb.GetCode(address)
}

// GetStorageAt mengambil nilai storage dari stateDB.
func (bc *Blockchain) GetStorageAt(address [20]byte, key [32]byte) [32]byte {
	sdb := bc.GetStateDB() // Sudah thread-safe
	if sdb == nil {
		logger.Warningf("GetStorageAt called but StateDB is nil for address %x", address)
		return [32]byte{}
	}
	return sdb.GetState(address, key) // Menggunakan GetState dari stateDB
}

// EstimateGas memberikan estimasi gas dasar untuk transaksi.
func (bc *Blockchain) EstimateGas(tx *Transaction) (uint64, error) {
	// Implementasi sederhana, bisa diperluas dengan simulasi EVM
	baseGas := uint64(21000) // Biaya dasar untuk transfer ETH
	if tx.GetTo() == nil {   // Pembuatan kontrak
		baseGas = 53000 // Biaya dasar pembuatan kontrak
	}
	data := tx.GetData()
	if len(data) > 0 {
		for _, b := range data {
			if b == 0 {
				baseGas += 4 // Biaya untuk byte nol dalam data
			} else {
				baseGas += 16 // Biaya untuk byte non-nol dalam data (Gtransaction Geth)
				// Di Ethereum sebenarnya 68 (Gtxdatanonzero) tapi disederhanakan di sini
			}
		}
	}
	return baseGas, nil
}

// GetDatabase mengembalikan instance database.
func (bc *Blockchain) GetDatabase() database.Database { return bc.db }

// SetVirtualMachine mengatur instance VM yang akan digunakan.
func (bc *Blockchain) SetVirtualMachine(vm interfaces.VirtualMachine) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.vm = vm
}

// SetConsensus mengatur instance mesin konsensus.
func (bc *Blockchain) SetConsensus(consensus interfaces.Engine) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.consensus = consensus
}

// GetConsensusEngine mengembalikan instance mesin konsensus.
func (bc *Blockchain) GetConsensusEngine() interfaces.Engine {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.consensus
}

// GetTotalDifficulty mengembalikan total kesulitan kumulatif dari chain.
func (bc *Blockchain) GetTotalDifficulty() *big.Int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if bc.totalDifficulty == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(bc.totalDifficulty) // Kembalikan salinan
}

// Pemeriksaan implementasi antarmuka untuk evm.Blockchain (jika Anda membuat antarmuka ini di paket evm)
var _ evm.Blockchain = (*Blockchain)(nil)
