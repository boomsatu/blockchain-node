package core

import (
	"blockchain-node/interfaces"
	"blockchain-node/logger" // Ditambahkan untuk logging hash
	"math/big"
	"sync"
	"time"
	// "fmt" // Tidak digunakan lagi secara eksplisit
)

type Miner struct {
	blockchain *Blockchain
	minerAddr  [20]byte
	running    bool
	mu         sync.Mutex
	stopChan   chan struct{}
	consensus  interfaces.Engine
}

func NewMiner(blockchain *Blockchain, minerAddr [20]byte, consensusEngine interfaces.Engine) *Miner {
	return &Miner{
		blockchain: blockchain,
		minerAddr:  minerAddr,
		consensus:  consensusEngine,
	}
}

func (m *Miner) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		logger.Info("Miner already running.")
		return
	}
	m.running = true
	m.stopChan = make(chan struct{}) // Buat channel baru setiap kali start
	m.mu.Unlock()

	logger.Infof("Starting miner for address: %x", m.minerAddr)

	go func() {
		for {
			select {
			case <-m.stopChan:
				logger.Info("Miner stopping work loop.")
				return
			default:
				m.mu.Lock()
				isRunning := m.running
				m.mu.Unlock()
				if !isRunning {
					logger.Info("Miner detected stop signal, exiting work loop.")
					return
				}

				m.mineBlock()
				// Jeda untuk memberi kesempatan CPU atau jika tidak ada transaksi
				// Sesuaikan durasi ini berdasarkan kebutuhan
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()
}

func (m *Miner) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		logger.Info("Miner is not running.")
		return
	}

	logger.Info("Stopping miner...")
	if m.stopChan != nil {
		close(m.stopChan)
		m.stopChan = nil // Set ke nil setelah ditutup untuk mencegah double close
	}
	m.running = false
	logger.Info("Miner stopped.")
}

func (m *Miner) mineBlock() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	parentBlock := m.blockchain.GetCurrentBlock()
	if parentBlock == nil {
		logger.Warning("Miner: No current block found, cannot mine. Waiting for genesis...")
		time.Sleep(1 * time.Second) // Kurangi waktu tunggu
		return
	}

	mempool := m.blockchain.GetMempool()
	// Menggunakan GetPendingTransactionsForBlock dari mempool.go
	// Pastikan metode ini sudah benar di mempool.go
	pendingTxs := mempool.GetPendingTransactionsForBlock(parentBlock.Header.GetGasLimit())

	if len(pendingTxs) == 0 {
		logger.Info("Miner: No pending transactions to mine. Attempting to mine an empty block.")
	}

	var nextDifficulty *big.Int
	if m.consensus != nil {
		// Dapatkan parent dari parent untuk kalkulasi difficulty yang lebih baik jika diperlukan
		// Untuk sekarang, kita gunakan parentBlock sebagai current dan parent untuk CalculateDifficulty
		// Ini mungkin perlu disesuaikan di implementasi CalculateDifficulty Anda
		var grandParentBlock interfaces.BlockConsensusItf = parentBlock // Default jika parent adalah genesis
		if parentBlock.Header.GetNumber() > 0 {
			gp := m.blockchain.GetBlockByHash(parentBlock.Header.GetParentHash())
			if gp != nil {
				grandParentBlock = gp
			}
		}
		nextDifficulty = m.consensus.CalculateDifficulty(parentBlock, grandParentBlock)
	} else {
		nextDifficulty = new(big.Int).Set(parentBlock.Header.GetDifficulty())
		logger.Warning("Miner: Consensus engine not set, using parent block's difficulty.")
	}

	if nextDifficulty.Sign() <= 0 {
		logger.Warningf("Miner: Calculated next difficulty is not positive (%s), using parent's difficulty %s.", nextDifficulty.String(), parentBlock.Header.GetDifficulty().String())
		nextDifficulty = new(big.Int).Set(parentBlock.Header.GetDifficulty())
		if nextDifficulty.Sign() <= 0 {
			nextDifficulty = big.NewInt(1000) // Fallback ke minimum jika parent juga invalid
			logger.Warningf("Miner: Parent difficulty also invalid, falling back to minimum difficulty %s.", nextDifficulty.String())
		}
	}

	newBlock := NewBlock(
		parentBlock.Header.GetHash(),
		parentBlock.Header.GetNumber()+1,
		m.minerAddr,
		nextDifficulty,
		m.blockchain.GetConfig().GetBlockGasLimit(), // Menggunakan metode dari interface
		pendingTxs,
	)

	// Tambahkan transaksi reward untuk miner
	// Transaksi Coinbase (reward) biasanya tidak memiliki 'From' yang valid atau signature,
	// dan nonce-nya bisa dianggap 0 atau tidak relevan untuk validasi standar.
	// GasPrice dan GasLimit juga bisa 0.
	rewardValueStr := "2000000000000000000" // 2 ETH
	rewardValue, _ := new(big.Int).SetString(rewardValueStr, 10)
	rewardTx := NewTransaction(
		0,             // Nonce untuk coinbase tx bisa 0
		&m.minerAddr,  // To: alamat miner
		rewardValue,   // Value: jumlah reward
		0,             // GasLimit: 0 untuk coinbase tx
		big.NewInt(0), // GasPrice: 0 untuk coinbase tx
		nil,           // Data: nil
	)
	// From untuk coinbase tx biasanya tidak diisi atau alamat khusus (misal, alamat nol)
	// Hash akan dihitung oleh NewTransaction. Tidak perlu Sign.
	newBlock.Transactions = append([]*Transaction{rewardTx}, newBlock.Transactions...)

	logger.Infof("Miner: Attempting to mine block %d with %d transactions (incl. reward). Difficulty: %s", newBlock.Header.Number, len(newBlock.Transactions), nextDifficulty.String())
	startTime := time.Now()

	if m.consensus == nil {
		logger.Error("Miner: Consensus engine is nil, cannot mine block.")
		return
	}

	if err := m.consensus.MineBlock(newBlock); err != nil {
		logger.Errorf("Miner: Failed to mine block %d: %v", newBlock.Header.Number, err)
		return
	}
	duration := time.Since(startTime)
	logger.Infof("Miner: Block %d mined in %v. Hash: %x", newBlock.Header.Number, duration, newBlock.Header.Hash)

	// AddBlock akan menangani eksekusi transaksi dan finalisasi header
	if err := m.blockchain.AddBlock(newBlock); err != nil {
		logger.Errorf("Miner: Failed to add mined block %d to blockchain: %v", newBlock.Header.Number, err)
		return
	}
	// Transaksi sudah dihapus dari mempool di dalam AddBlock
	logger.Infof("Miner: Successfully added block %d to blockchain.", newBlock.Header.Number)
}

func (m *Miner) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}
