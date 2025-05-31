package core

import (
	"blockchain-node/crypto"
	"blockchain-node/interfaces" // Pastikan ini diimpor jika BlockHeader mengimplementasikan interface dari sini
	"encoding/json"
	"math/big"
	"time"
)

// BlockHeader mendefinisikan struktur header sebuah blok.
type BlockHeader struct {
	Number      uint64   `json:"number"`
	ParentHash  [32]byte `json:"parentHash"`
	Timestamp   int64    `json:"timestamp"`
	StateRoot   [32]byte `json:"stateRoot"`
	TxHash      [32]byte `json:"transactionsRoot"` // Akar hash dari transaksi
	ReceiptHash [32]byte `json:"receiptsRoot"`     // Akar hash dari receipts
	LogsBloom   []byte   `json:"logsBloom,omitempty"`
	GasLimit    uint64   `json:"gasLimit"`
	GasUsed     uint64   `json:"gasUsed"`
	Difficulty  *big.Int `json:"difficulty"`
	Nonce       uint64   `json:"nonce"`               // Nonce untuk PoW
	Miner       [20]byte `json:"miner"`               // Alamat miner
	MixHash     [32]byte `json:"mixHash"`             // BARU: Hash campuran untuk PoW (Ethash)
	ExtraData   []byte   `json:"extraData,omitempty"` // BARU: Data tambahan opsional
	Hash        [32]byte `json:"hash"`                // Hash dari header ini
}

// Implementasi interfaces.BlockHeaderItf untuk *BlockHeader
func (bh *BlockHeader) GetNumber() uint64       { return bh.Number }
func (bh *BlockHeader) GetParentHash() [32]byte { return bh.ParentHash }
func (bh *BlockHeader) GetTimestamp() int64     { return bh.Timestamp }
func (bh *BlockHeader) GetDifficulty() *big.Int { return new(big.Int).Set(bh.Difficulty) } // Kembalikan salinan
func (bh *BlockHeader) GetGasLimit() uint64     { return bh.GasLimit }
func (bh *BlockHeader) GetMiner() [20]byte      { return bh.Miner }
func (bh *BlockHeader) GetHash() [32]byte       { return bh.Hash }

// Implementasi interfaces.BlockHeaderConsensusItf untuk *BlockHeader
func (bh *BlockHeader) GetNonce() uint64         { return bh.Nonce }
func (bh *BlockHeader) SetDifficulty(d *big.Int) { bh.Difficulty.Set(d) }
func (bh *BlockHeader) SetHash(h [32]byte)       { bh.Hash = h }
func (bh *BlockHeader) SetNonce(n uint64)        { bh.Nonce = n }

// Block mendefinisikan struktur sebuah blok dalam blockchain.
type Block struct {
	Header       *BlockHeader          `json:"header"`
	Transactions []*Transaction        `json:"transactions"`
	Receipts     []*TransactionReceipt `json:"receipts,omitempty"` // Receipts bisa jadi opsional saat serialisasi awal
	// Uncles       []*BlockHeader        `json:"uncles,omitempty"` // Jika Anda mendukung uncle block
}

// Implementasi interfaces.BlockConsensusItf untuk *Block
func (b *Block) GetHeader() interfaces.BlockHeaderConsensusItf { return b.Header }

// CalculateHash menghitung hash dari header blok.
// Hash header dihitung tanpa field Hash itu sendiri.
func (b *Block) CalculateHash() [32]byte {
	// Buat salinan header untuk di-hash, dengan field Hash dikosongkan
	headerToHash := *b.Header
	headerToHash.Hash = [32]byte{} // Kosongkan field Hash sebelum marshalling untuk hashing

	jsonData, err := json.Marshal(headerToHash)
	if err != nil {
		// Ini seharusnya tidak terjadi jika struct valid.
		// Pertimbangkan panic atau logging error fatal.
		// fmt.Printf("Error marshalling header for hashing: %v\n", err)
		return [32]byte{}
	}
	return crypto.Keccak256Hash(jsonData)
}

// NewBlock membuat instance blok baru.
// Hash blok akan dihitung setelah semua field header (termasuk StateRoot, TxHash, ReceiptHash, GasUsed) diisi.
func NewBlock(parentHash [32]byte, number uint64, miner [20]byte, difficulty *big.Int, gasLimit uint64, transactions []*Transaction) *Block {
	header := &BlockHeader{
		Number:     number,
		ParentHash: parentHash,
		Timestamp:  time.Now().Unix(), // Timestamp saat pembuatan blok
		GasLimit:   gasLimit,
		Difficulty: new(big.Int).Set(difficulty), // Pastikan difficulty disalin
		Miner:      miner,
		// MixHash dan ExtraData bisa diisi oleh proses mining jika relevan dengan algoritma PoW Anda
		// atau dari genesis spec.
	}

	block := &Block{
		Header:       header,
		Transactions: transactions,
		Receipts:     []*TransactionReceipt{}, // Inisialisasi slice kosong
	}
	// Hash belum dihitung di sini; akan dihitung setelah StateRoot, dll., diisi.
	return block
}

// ToJSON serializes the block to JSON.
func (b *Block) ToJSON() ([]byte, error) {
	return json.Marshal(b)
}

// FromJSON deserializes a block from JSON.
func BlockFromJSON(data []byte) (*Block, error) {
	var block Block
	err := json.Unmarshal(data, &block)
	if err != nil {
		return nil, err
	}
	return &block, nil
}
