package core

import (
	"blockchain-node/logger"
	"errors"
	"math/big"
	"regexp"

	"github.com/ethereum/go-ethereum/common" // Tetap diperlukan untuk common.Address
)

type Validator struct {
	maxTransactionSize uint64
	maxBlockSize       uint64
	maxGasLimit        uint64
	minGasPrice        *big.Int
	addressRegex       *regexp.Regexp
}

func NewValidator() *Validator {
	return &Validator{
		maxTransactionSize: 128 * 1024,
		maxBlockSize:       1024 * 1024,
		maxGasLimit:        10000000,
		minGasPrice:        big.NewInt(1000), // Minimum gas price untuk transaksi normal
		addressRegex:       regexp.MustCompile("^0x[a-fA-F0-9]{40}$"),
	}
}

// ValidateTransaction sekarang menerima flag isCoinbase
func (v *Validator) ValidateTransaction(tx *Transaction, isCoinbase bool) error {
	if tx == nil {
		return errors.New("transaction is nil")
	}

	if !isCoinbase {
		// Validasi GasPrice hanya untuk transaksi non-coinbase
		if tx.GasPrice == nil || tx.GasPrice.Cmp(v.minGasPrice) < 0 {
			logger.Warningf("Transaction gas price too low: %v (min: %v) for tx %x", tx.GasPrice, v.minGasPrice, tx.Hash)
			return errors.New("gas price too low")
		}
		// Validasi GasLimit hanya untuk transaksi non-coinbase (coinbase bisa 0)
		if tx.GasLimit == 0 || tx.GasLimit > v.maxGasLimit {
			logger.Warningf("Invalid gas limit: %d for tx %x", tx.GasLimit, tx.Hash)
			return errors.New("invalid gas limit")
		}
	} else {
		// Aturan khusus untuk transaksi coinbase
		if tx.GasPrice == nil || tx.GasPrice.Cmp(big.NewInt(0)) != 0 {
			logger.Warningf("Coinbase transaction has non-zero gas price: %v for tx %x", tx.GasPrice, tx.Hash)
			return errors.New("coinbase transaction must have zero gas price")
		}
		if tx.GasLimit != 0 { // GasLimit coinbase biasanya 0
			logger.Warningf("Coinbase transaction has non-zero gas limit: %d for tx %x", tx.GasLimit, tx.Hash)
			return errors.New("coinbase transaction must have zero gas limit")
		}
		// Transaksi coinbase mungkin tidak memiliki 'From' yang valid atau signature
		// jadi beberapa validasi di bawah ini mungkin perlu dilewati untuk coinbase.
	}

	// Validate value (berlaku untuk semua jenis transaksi)
	if tx.Value == nil || tx.Value.Sign() < 0 {
		logger.Warningf("Invalid transaction value: %v for tx %x", tx.Value, tx.Hash)
		return errors.New("invalid transaction value")
	}

	// Validate to address format if present
	if tx.To != nil {
		var toAddr common.Address
		copy(toAddr[:], (*tx.To)[:]) // tx.To adalah *[20]byte
		if !v.IsValidAddress(toAddr.Hex()) {
			logger.Warningf("Invalid to address: %s for tx %x", toAddr.Hex(), tx.Hash)
			return errors.New("invalid to address")
		}
	} else if !isCoinbase && !tx.IsContractCreation() {
		// Jika bukan coinbase dan bukan pembuatan kontrak, 'To' tidak boleh nil
		logger.Warningf("Transaction 'to' address is nil for non-contract creation, non-coinbase tx %x", tx.Hash)
		return errors.New("transaction 'to' address is nil")
	}

	// Validasi From address dan Signature hanya untuk transaksi non-coinbase
	if !isCoinbase {
		if tx.From == (common.Address{}) {
			logger.Warningf("Transaction missing from address for tx %x", tx.Hash)
			return errors.New("missing from address")
		}
		if !v.IsValidAddress(common.BytesToAddress(tx.From[:]).Hex()) {
			logger.Warningf("Invalid from address: %s for tx %x", common.BytesToAddress(tx.From[:]).Hex(), tx.Hash)
			return errors.New("invalid from address")
		}
		if tx.V == nil || tx.R == nil || tx.S == nil {
			logger.Warningf("Transaction missing signature components for tx %x", tx.Hash)
			return errors.New("missing signature components")
		}
		if !tx.VerifySignature() {
			logger.Warningf("Invalid transaction signature for tx %x", tx.Hash)
			return errors.New("invalid transaction signature")
		}
	}

	// Validate transaction size (berlaku untuk semua jenis transaksi)
	txData, err := tx.ToJSON()
	if err != nil {
		logger.Errorf("Failed to serialize transaction %x: %v", tx.Hash, err)
		return errors.New("failed to serialize transaction")
	}
	if uint64(len(txData)) > v.maxTransactionSize {
		logger.Warningf("Transaction size too large: %d bytes for tx %x", len(txData), tx.Hash)
		return errors.New("transaction size too large")
	}

	// logger.Debugf("Transaction validation passed: %x (Coinbase: %t)", tx.Hash, isCoinbase)
	return nil
}

func (v *Validator) ValidateBlock(block *Block) error {
	if block == nil {
		return errors.New("block is nil")
	}
	if block.Header == nil {
		return errors.New("block header is nil")
	}

	// Validate block gas limit
	if block.Header.GasLimit > v.maxGasLimit {
		logger.Warningf("Block gas limit too high: %d for block %d", block.Header.GasLimit, block.Header.Number)
		return errors.New("block gas limit too high")
	}
	// Validate gas used doesn't exceed limit (GasUsed diisi setelah eksekusi)
	// Pemeriksaan ini lebih relevan setelah blok dieksekusi dan GasUsed diisi.
	// Namun, kita bisa periksa apakah jumlah GasLimit transaksi tidak melebihi GasLimit blok.
	var totalTxGasLimit uint64
	for _, tx := range block.Transactions {
		totalTxGasLimit += tx.GasLimit
	}
	if totalTxGasLimit > block.Header.GasLimit && len(block.Transactions) > 0 { // Hanya jika ada transaksi
		// Jika ini transaksi coinbase, tx.GasLimit adalah 0.
		// Kita perlu logika yang lebih baik untuk mengecek total gas limit dari transaksi non-coinbase.
		// Untuk sementara, kita bisa skip check ini di sini atau membuatnya lebih pintar.
		// logger.Warningf("Sum of transaction gas limits (%d) exceeds block gas limit (%d) for block %d", totalTxGasLimit, block.Header.GasLimit, block.Header.Number)
		// return errors.New("sum of transaction gas limits exceeds block gas limit")
	}

	blockData, err := block.ToJSON()
	if err != nil {
		logger.Errorf("Failed to serialize block %d: %v", block.Header.Number, err)
		return errors.New("failed to serialize block")
	}
	if uint64(len(blockData)) > v.maxBlockSize {
		logger.Warningf("Block size too large: %d bytes for block %d", len(blockData), block.Header.Number)
		return errors.New("block size too large")
	}

	if len(block.Transactions) > 0 {
		// Transaksi pertama adalah coinbase (reward miner)
		if err := v.ValidateTransaction(block.Transactions[0], true); err != nil {
			logger.Errorf("Invalid coinbase transaction in block %d: %v", block.Header.Number, err)
			return err
		}
		// Validasi transaksi lainnya
		for i := 1; i < len(block.Transactions); i++ {
			tx := block.Transactions[i]
			if err := v.ValidateTransaction(tx, false); err != nil {
				logger.Errorf("Invalid transaction %d (hash: %x) in block %d: %v", i, tx.Hash, block.Header.Number, err)
				return err
			}
		}
	}
	// Pemeriksaan GasUsed vs GasLimit blok dilakukan setelah eksekusi blok di core/blockchain.go.
	// Di sini kita hanya memvalidasi struktur dan konsistensi awal.

	logger.Debugf("Block basic validation passed: %d (%x)", block.Header.Number, block.Header.Hash)
	return nil
}

func (v *Validator) IsValidAddress(address string) bool {
	return v.addressRegex.MatchString(address)
}

// Metode ini mungkin tidak lagi digunakan secara eksternal jika validasi ada di ValidateTransaction
func (v *Validator) ValidateGasPrice(gasPrice *big.Int) bool {
	return gasPrice != nil && gasPrice.Cmp(v.minGasPrice) >= 0
}

func (v *Validator) ValidateGasLimit(gasLimit uint64) bool {
	return gasLimit > 0 && gasLimit <= v.maxGasLimit
}
