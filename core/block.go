package core

import (
	"blockchain-node/crypto"
	"blockchain-node/interfaces"
	"encoding/json"
	"math/big"
	"time"
)

type BlockHeader struct {
	Number      uint64   `json:"number"`
	ParentHash  [32]byte `json:"parentHash"`
	Timestamp   int64    `json:"timestamp"`
	StateRoot   [32]byte `json:"stateRoot"`
	TxHash      [32]byte `json:"transactionsRoot"`
	ReceiptHash [32]byte `json:"receiptsRoot"`
	LogsBloom   []byte   `json:"logsBloom,omitempty"`
	GasLimit    uint64   `json:"gasLimit"`
	GasUsed     uint64   `json:"gasUsed"`
	Difficulty  *big.Int `json:"difficulty"`
	Nonce       uint64   `json:"nonce"` // Field Nonce sudah ada
	Miner       [20]byte `json:"miner"`
	Hash        [32]byte `json:"hash"`
}

// Implementasi interfaces.BlockHeaderItf untuk *BlockHeader
func (bh *BlockHeader) GetNumber() uint64       { return bh.Number }
func (bh *BlockHeader) GetParentHash() [32]byte { return bh.ParentHash }
func (bh *BlockHeader) GetTimestamp() int64     { return bh.Timestamp }
func (bh *BlockHeader) GetDifficulty() *big.Int { return new(big.Int).Set(bh.Difficulty) }
func (bh *BlockHeader) GetGasLimit() uint64     { return bh.GasLimit }
func (bh *BlockHeader) GetMiner() [20]byte      { return bh.Miner }
func (bh *BlockHeader) GetHash() [32]byte       { return bh.Hash }

// Implementasi interfaces.BlockHeaderConsensusItf untuk *BlockHeader
// Metode GetNonce() ditambahkan di sini
func (bh *BlockHeader) GetNonce() uint64         { return bh.Nonce }
func (bh *BlockHeader) SetDifficulty(d *big.Int) { bh.Difficulty.Set(d) }
func (bh *BlockHeader) SetHash(h [32]byte)       { bh.Hash = h }
func (bh *BlockHeader) SetNonce(n uint64)        { bh.Nonce = n }

type Block struct {
	Header       *BlockHeader          `json:"header"`
	Transactions []*Transaction        `json:"transactions"`
	Receipts     []*TransactionReceipt `json:"receipts"`
}

// Implementasi interfaces.BlockConsensusItf untuk *Block
func (b *Block) GetHeader() interfaces.BlockHeaderConsensusItf { return b.Header }

func (b *Block) CalculateHash() [32]byte {
	headerToHash := *b.Header
	headerToHash.Hash = [32]byte{}

	jsonData, err := json.Marshal(headerToHash)
	if err != nil {
		return [32]byte{}
	}
	return crypto.Keccak256Hash(jsonData)
}

func NewBlock(parentHash [32]byte, number uint64, miner [20]byte, difficulty *big.Int, gasLimit uint64, transactions []*Transaction) *Block {
	header := &BlockHeader{
		Number:     number,
		ParentHash: parentHash,
		Timestamp:  time.Now().Unix(),
		GasLimit:   gasLimit,
		Difficulty: new(big.Int).Set(difficulty), // Pastikan difficulty disalin
		Miner:      miner,
	}

	block := &Block{
		Header:       header,
		Transactions: transactions,
		Receipts:     []*TransactionReceipt{},
	}
	return block
}

func (b *Block) ToJSON() ([]byte, error) {
	return json.Marshal(b)
}
