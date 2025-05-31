package core

import (
	"blockchain-node/crypto"
	"fmt"

	// "blockchain-node/interfaces" // Tidak perlu impor interfaces di sini
	"encoding/hex"
	"encoding/json"
	"math/big"
	// "fmt" // Jika diperlukan untuk debugging
)

type Transaction struct {
	Nonce    uint64    `json:"nonce"`
	GasPrice *big.Int  `json:"gasPrice"`
	GasLimit uint64    `json:"gasLimit"`
	To       *[20]byte `json:"to"` // Pointer karena bisa nil (contract creation)
	Value    *big.Int  `json:"value"`
	Data     []byte    `json:"data"`
	V        *big.Int  `json:"v"`
	R        *big.Int  `json:"r"`
	S        *big.Int  `json:"s"`
	Hash     [32]byte  `json:"hash"` // Cache hash transaksi
	From     [20]byte  `json:"from"` // Alamat pengirim, diisi setelah penandatanganan
}

// Implementasi interfaces.TransactionItf untuk *Transaction
func (tx *Transaction) GetHash() [32]byte        { return tx.Hash }
func (tx *Transaction) GetFrom() [20]byte        { return tx.From }
func (tx *Transaction) GetTo() *[20]byte         { return tx.To }
func (tx *Transaction) GetValue() *big.Int       { return new(big.Int).Set(tx.Value) } // Kembalikan salinan
func (tx *Transaction) GetData() []byte          { return tx.Data }
func (tx *Transaction) GetNonce() uint64         { return tx.Nonce }
func (tx *Transaction) GetGasPrice() *big.Int    { return new(big.Int).Set(tx.GasPrice) } // Kembalikan salinan
func (tx *Transaction) GetGasLimit() uint64      { return tx.GasLimit }
func (tx *Transaction) IsContractCreation() bool { return tx.To == nil }

// NewTransaction membuat instance transaksi baru. Hash dihitung saat pembuatan.
func NewTransaction(nonce uint64, to *[20]byte, value *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *Transaction {
	if value == nil {
		value = big.NewInt(0)
	}
	if gasPrice == nil {
		gasPrice = big.NewInt(0) // Atau default gas price
	}
	tx := &Transaction{
		Nonce:    nonce,
		GasPrice: gasPrice,
		GasLimit: gasLimit,
		To:       to,
		Value:    value,
		Data:     data,
		// V, R, S, From, Hash akan diisi nanti
	}
	tx.Hash = tx.calculateHashInternal() // Hitung hash internal saat pembuatan
	return tx
}

// calculateHashInternal adalah versi privat untuk menghitung hash sebelum ditandatangani.
// Ini tidak menyertakan V, R, S, atau From.
func (tx *Transaction) calculateHashInternal() [32]byte {
	// Data yang di-hash untuk penandatanganan tidak boleh menyertakan signature (V,R,S) atau From.
	// Biasanya ini adalah RLP encoding dari (Nonce, GasPrice, GasLimit, To, Value, Data, ChainID, 0, 0) untuk EIP-155.
	// Untuk simplifikasi, kita akan JSON marshal field-field inti.
	// Penting: Untuk kompatibilitas nyata, gunakan RLP encoding yang sesuai.
	type txDataForHashing struct {
		Nonce    uint64 `json:"nonce"`
		GasPrice string `json:"gasPrice"`
		GasLimit uint64 `json:"gasLimit"`
		To       string `json:"to"` // string hex
		Value    string `json:"value"`
		Data     string `json:"data"` // string hex
		// ChainID uint64 `json:"chainId"` // Jika menggunakan EIP-155
	}

	toHex := ""
	if tx.To != nil {
		toHex = hex.EncodeToString(tx.To[:])
	}

	hashData := txDataForHashing{
		Nonce:    tx.Nonce,
		GasPrice: tx.GasPrice.String(),
		GasLimit: tx.GasLimit,
		To:       toHex,
		Value:    tx.Value.String(),
		Data:     hex.EncodeToString(tx.Data),
		// ChainID:  1337, // Ganti dengan ChainID aktual jika digunakan
	}

	jsonData, _ := json.Marshal(hashData)
	return crypto.Keccak256Hash(jsonData)
}

func (tx *Transaction) Sign(privateKeyBytes []byte) error {
	// Hash yang akan ditandatangani adalah hash internal yang tidak termasuk signature.
	signingHash := tx.calculateHashInternal()

	sig, err := crypto.Sign(signingHash[:], privateKeyBytes)
	if err != nil {
		return err
	}

	tx.R = new(big.Int).SetBytes(sig[:32])
	tx.S = new(big.Int).SetBytes(sig[32:64])
	tx.V = new(big.Int).SetInt64(int64(sig[64]) + 27) // EIP-155 bisa lebih kompleks

	// Setelah ditandatangani, alamat pengirim bisa di-recover.
	// Dan hash final transaksi (yang mungkin termasuk V,R,S untuk beberapa definisi) bisa dihitung.
	// Untuk Ethereum, hash transaksi dihitung dari RLP encoding (Nonce, GasPrice, GasLimit, To, Value, Data, V, R, S).
	// Kita akan tetap menggunakan hash internal sebagai identifier utama untuk simplifikasi.
	// tx.Hash = tx.calculateHashWithSignature() // Jika definisi hash transaksi Anda berbeda setelah ditandatangani

	// Recover 'From' address
	fromAddr, err := crypto.RecoverAddress(signingHash[:], sig)
	if err != nil {
		return fmt.Errorf("failed to recover address from signature: %v", err)
	}
	tx.From = fromAddr
	return nil
}

func (tx *Transaction) VerifySignature() bool {
	if tx.R == nil || tx.S == nil || tx.V == nil || (tx.From == [20]byte{}) {
		return false
	}

	signingHash := tx.calculateHashInternal() // Hash yang sama yang digunakan untuk menandatangani

	sig := make([]byte, 65)
	copy(sig[0:32], tx.R.Bytes())
	copy(sig[32:64], tx.S.Bytes())
	sig[64] = byte(tx.V.Int64() - 27) // Asumsi V sederhana, perlu penyesuaian untuk EIP-155

	recoveredAddr, err := crypto.RecoverAddress(signingHash[:], sig)
	if err != nil {
		return false
	}
	return recoveredAddr == tx.From
}

func (tx *Transaction) ToJSON() ([]byte, error) {
	return json.Marshal(tx)
}

// TransactionReceipt dan Log struct (asumsi sudah ada atau didefinisikan di core)
// type TransactionReceipt struct { ... }
// type Log struct { ... }
