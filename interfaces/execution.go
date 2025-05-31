package interfaces

import (
	customState "blockchain-node/state" // Tetap dibutuhkan untuk StateDB di ExecutionContext
	"math/big"

	"github.com/ethereum/go-ethereum/params" // Untuk params.ChainConfig
)

// ChainConfigItf mendefinisikan metode yang dibutuhkan dari konfigurasi chain.
type ChainConfigItf interface {
	ToEthChainConfig() *params.ChainConfig
	GetChainID() uint64
	GetBlockGasLimit() uint64
	// Tambahkan metode lain dari config jika dibutuhkan oleh EVM atau komponen lain
}

// TransactionItf mendefinisikan metode yang dibutuhkan dari sebuah transaksi.
type TransactionItf interface {
	GetHash() [32]byte
	GetFrom() [20]byte
	GetTo() *[20]byte
	GetValue() *big.Int
	GetData() []byte
	GetNonce() uint64
	GetGasPrice() *big.Int
	GetGasLimit() uint64
	IsContractCreation() bool
	// Tambahkan metode lain yang mungkin dibutuhkan oleh VM dari transaksi
}

// BlockHeaderItf mendefinisikan metode yang dibutuhkan dari sebuah block header.
type BlockHeaderItf interface {
	GetNumber() uint64
	GetParentHash() [32]byte // Ditambahkan untuk GetHashFn
	GetTimestamp() int64
	GetDifficulty() *big.Int
	GetGasLimit() uint64
	GetMiner() [20]byte
	GetHash() [32]byte // Ditambahkan untuk GetHashFn
	// Tambahkan metode lain yang mungkin dibutuhkan oleh VM dari block header
}

// Log (sebelumnya ExecutionLog)
type Log struct {
	Address [20]byte
	Topics  [][32]byte
	Data    []byte
	// Informasi terkait blok/tx (BlockNumber, TxHash, dll.) akan ditambahkan
	// saat membuat TransactionReceipt di package 'core'.
}

// ExecutionContext sekarang menggunakan interface yang didefinisikan secara lokal.
type ExecutionContext struct {
	Transaction TransactionItf
	BlockHeader BlockHeaderItf
	StateDB     *customState.StateDB // StateDB kustom Anda
	GasUsedPool *big.Int             // Pointer ke akumulator gas yang digunakan di blok
}

// ExecutionResult
type ExecutionResult struct {
	GasUsed         uint64
	ContractAddress *[20]byte
	Logs            []Log // Menggunakan Log dari package ini
	Status          uint64
	Err             error
	ReturnData      []byte
}

// VirtualMachine interface tetap sama.
type VirtualMachine interface {
	ExecuteTransaction(ctx *ExecutionContext) (*ExecutionResult, error)
}
