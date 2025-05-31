package interfaces

import (
	"math/big"
)

// BlockHeaderConsensusItf
type BlockHeaderConsensusItf interface {
	GetNumber() uint64
	GetParentHash() [32]byte
	GetTimestamp() int64
	GetDifficulty() *big.Int
	SetDifficulty(*big.Int)
	GetHash() [32]byte
	SetHash([32]byte)
	GetNonce() uint64
	SetNonce(uint64)
}

// BlockConsensusItf
type BlockConsensusItf interface {
	GetHeader() BlockHeaderConsensusItf
	CalculateHash() [32]byte // Diperlukan untuk PoW
	// GetTransactions() []TransactionItf // Jika konsensus perlu akses ke transaksi
}

// Engine interface
type Engine interface {
	MineBlock(block BlockConsensusItf) error
	ValidateProofOfWork(block BlockConsensusItf) bool
	CalculateDifficulty(currentBlock BlockConsensusItf, parentBlock BlockConsensusItf) *big.Int
}
