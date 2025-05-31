package interfaces

import (
	customState "blockchain-node/state" // Tetap dibutuhkan untuk StateDB di ExecutionContext
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types" // Diperlukan untuk types.Log dari Geth
	"github.com/ethereum/go-ethereum/params"     // Untuk params.ChainConfig
)

// ChainConfigItf mendefinisikan metode yang dibutuhkan dari konfigurasi chain.
type ChainConfigItf interface {
	ToEthChainConfig() *params.ChainConfig
	GetChainID() uint64
	GetBlockGasLimit() uint64
}

// TransactionItf mendefinisikan metode yang dibutuhkan dari sebuah transaksi.
type TransactionItf interface {
	GetHash() [32]byte
	GetFrom() [20]byte
	GetTo() *[20]byte
	GetValue() *big.Int // Akan dikonversi ke uint256.Int saat interaksi dengan EVM Geth
	GetData() []byte
	GetNonce() uint64
	GetGasPrice() *big.Int // Akan dikonversi ke uint256.Int saat interaksi dengan EVM Geth
	GetGasLimit() uint64
	IsContractCreation() bool
}

// BlockHeaderItf mendefinisikan metode yang dibutuhkan dari sebuah block header.
type BlockHeaderItf interface {
	GetNumber() uint64
	GetParentHash() [32]byte
	GetTimestamp() int64 // Akan digunakan sebagai uint64 untuk vm.BlockContext.Time
	GetDifficulty() *big.Int
	GetGasLimit() uint64
	GetMiner() [20]byte
	GetHash() [32]byte
}

// Log adalah representasi log yang digunakan dalam ExecutionResult.
type Log struct {
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	Data        []byte         `json:"data"`
	BlockNumber uint64         `json:"blockNumber"`
	TxHash      common.Hash    `json:"transactionHash"`
	TxIndex     uint           `json:"transactionIndex"`
	BlockHash   common.Hash    `json:"blockHash"`
	Index       uint           `json:"logIndex"`
}

// ExecutionContext sekarang menggunakan interface yang didefinisikan secara lokal.
type ExecutionContext struct {
	Transaction TransactionItf
	BlockHeader BlockHeaderItf
	StateDB     *customState.StateDB
	GasUsedPool *big.Int
}

// ExecutionResult
type ExecutionResult struct {
	GasUsed         uint64
	ContractAddress *[20]byte
	Logs            []*Log // Diubah kembali ke slice of pointers
	Status          uint64
	Err             error
	ReturnData      []byte
}

// VirtualMachine interface tetap sama.
type VirtualMachine interface {
	ExecuteTransaction(ctx *ExecutionContext) (*ExecutionResult, error)
}

// ToInterfacesLog mengkonversi types.Log dari Geth ke *interfaces.Log kita.
func ToInterfacesLog(gethLog *types.Log) *Log { // Mengembalikan *Log (pointer)
	topics := make([]common.Hash, len(gethLog.Topics))
	for i, t := range gethLog.Topics {
		topics[i] = t
	}
	return &Log{ // Mengembalikan pointer ke struct
		Address:     gethLog.Address,
		Topics:      topics,
		Data:        gethLog.Data,
		BlockNumber: gethLog.BlockNumber,
		TxHash:      gethLog.TxHash,
		TxIndex:     gethLog.TxIndex,
		BlockHash:   gethLog.BlockHash,
		Index:       gethLog.Index,
	}
}
