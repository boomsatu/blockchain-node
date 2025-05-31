package core

import (
	// "blockchain-node/interfaces" // Tidak dibutuhkan di sini
	"errors"
	"sync"
)

// Asumsi Transaction sudah didefinisikan di package core
// type Transaction struct { ... }

type Mempool struct {
	transactions map[[32]byte]*Transaction
	pending      map[[20]byte][]*Transaction // Opsional, untuk melacak tx per sender
	mu           sync.RWMutex
	// Tambahkan batasan ukuran mempool jika perlu
	// maxTransactions int
}

func NewMempool() *Mempool {
	return &Mempool{
		transactions: make(map[[32]byte]*Transaction),
		pending:      make(map[[20]byte][]*Transaction),
		// maxTransactions: 1000, // Contoh batasan
	}
}

func (mp *Mempool) AddTransaction(tx *Transaction) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// if len(mp.transactions) >= mp.maxTransactions {
	// 	return errors.New("mempool is full")
	// }

	if _, exists := mp.transactions[tx.Hash]; exists {
		return errors.New("transaction already exists in mempool")
	}

	// Validasi dasar bisa dilakukan di sini atau sebelum masuk mempool
	// if !tx.VerifySignature() { // Asumsi VerifySignature ada di Transaction
	// 	return errors.New("invalid transaction signature")
	// }

	mp.transactions[tx.Hash] = tx
	// mp.pending[tx.From] = append(mp.pending[tx.From], tx) // Jika tx.From sudah ada

	return nil
}

func (mp *Mempool) GetTransaction(hash [32]byte) *Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.transactions[hash]
}

// GetPendingTransactions mengembalikan semua transaksi yang ada di mempool.
// Miner akan mengambil dari sini dan mungkin melakukan filtering tambahan.
func (mp *Mempool) GetPendingTransactions() []*Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	txs := make([]*Transaction, 0, len(mp.transactions))
	for _, tx := range mp.transactions {
		txs = append(txs, tx)
	}
	// Urutkan berdasarkan nonce atau gas price jika perlu
	return txs
}

// GetPendingTransactionsForBlock adalah contoh bagaimana Anda bisa mengimplementasikannya
// jika ingin memfilter transaksi berdasarkan gas limit blok.
func (mp *Mempool) GetPendingTransactionsForBlock(blockGasLimit uint64) []*Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	// Implementasi yang lebih canggih akan mengurutkan berdasarkan gas price, nonce, dll.
	// dan memilih transaksi hingga gas limit blok terpenuhi.
	// Untuk sekarang, kita kembalikan semua, dan miner bisa membatasi jumlahnya.

	txs := make([]*Transaction, 0, len(mp.transactions))
	var currentGas uint64 = 0

	// Contoh sederhana: ambil transaksi selama total gasnya tidak melebihi limit blok
	// Ini memerlukan pengurutan transaksi yang baik (misalnya berdasarkan GasPrice tertinggi)
	// Untuk simplifikasi, kita ambil saja beberapa transaksi pertama.
	// Anda perlu logika yang lebih baik di sini untuk pemilihan transaksi yang optimal.

	tempTxs := make([]*Transaction, 0, len(mp.transactions))
	for _, tx := range mp.transactions {
		tempTxs = append(tempTxs, tx)
	}
	// TODO: Urutkan tempTxs berdasarkan prioritas (misal, GasPrice DESC, Nonce ASC)

	for _, tx := range tempTxs {
		if currentGas+tx.GetGasLimit() <= blockGasLimit {
			txs = append(txs, tx)
			currentGas += tx.GetGasLimit()
		} else {
			// Jika satu transaksi saja sudah melebihi limit, kita mungkin tetap ingin mencoba memasukkannya
			// jika itu adalah satu-satunya transaksi. Tapi untuk pemilihan banyak transaksi, ini akan berhenti.
			// Untuk sekarang, kita berhenti jika transaksi berikutnya akan melebihi limit.
			break
		}
	}
	return txs
}

func (mp *Mempool) RemoveTransaction(hash [32]byte) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	tx, exists := mp.transactions[hash]
	if exists {
		delete(mp.transactions, hash)
		// Hapus juga dari mp.pending jika digunakan
		if tx != nil && len(mp.pending[tx.From]) > 0 {
			newPending := []*Transaction{}
			for _, pendingTx := range mp.pending[tx.From] {
				if pendingTx.Hash != hash {
					newPending = append(newPending, pendingTx)
				}
			}
			if len(newPending) == 0 {
				delete(mp.pending, tx.From)
			} else {
				mp.pending[tx.From] = newPending
			}
		}
	}
}

func (mp *Mempool) Clear() {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.transactions = make(map[[32]byte]*Transaction)
	mp.pending = make(map[[20]byte][]*Transaction)
}

func (mp *Mempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.transactions)
}
