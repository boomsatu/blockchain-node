package wallet

import (
	"blockchain-node/core"
	"blockchain-node/crypto"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
)

type Wallet struct {
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
	address    [20]byte
}

func NewWallet() (*Wallet, error) {
	privateKey, publicKey, err := crypto.GenerateEthKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %v", err)
	}

	address := crypto.PubkeyToAddress(publicKey)

	return &Wallet{
		privateKey: privateKey,
		publicKey:  publicKey,
		address:    address,
	}, nil
}

func NewWalletFromPrivateKey(privateKeyHex string) (*Wallet, error) {
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key format: %v", err)
	}

	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}

	publicKey := &privateKey.PublicKey
	address := crypto.PubkeyToAddress(publicKey)

	return &Wallet{
		privateKey: privateKey,
		publicKey:  publicKey,
		address:    address,
	}, nil
}

func (w *Wallet) GetAddress() string {
	return hex.EncodeToString(w.address[:])
}

func (w *Wallet) GetAddressBytes() [20]byte {
	return w.address
}

func (w *Wallet) GetPrivateKey() *ecdsa.PrivateKey {
	return w.privateKey
}

func (w *Wallet) GetPrivateKeyHex() string {
	return hex.EncodeToString(crypto.FromECDSA(w.privateKey))
}

func (w *Wallet) GetPublicKey() *ecdsa.PublicKey {
	return w.publicKey
}

func (w *Wallet) GetPublicKeyHex() string {
	return hex.EncodeToString(crypto.FromECDSAPub(w.publicKey))
}

func (w *Wallet) SignData(data []byte) ([]byte, error) {
	hash := crypto.Keccak256Hash(data)
	return crypto.Sign(hash[:], crypto.FromECDSA(w.privateKey))
}

func (w *Wallet) SignTransaction(tx *core.Transaction) error {
	// Asumsi tx.From akan diisi di sini setelah penandatanganan
	// atau tx.Sign akan mengembalikan alamat pengirim yang bisa di-assign ke tx.From
	// Untuk konsistensi dengan VerifySignature, tx.Sign harus mengisi tx.From
	err := tx.Sign(crypto.FromECDSA(w.privateKey))
	if err != nil {
		return err
	}
	// Pastikan tx.From diisi dengan benar oleh tx.Sign
	// Jika tidak, Anda mungkin perlu melakukannya di sini:
	// hash := tx.CalculateHash()
	// recoveredAddr, _ := crypto.RecoverAddress(hash[:], reconstructedSignature) // Anda perlu signature di sini
	// tx.From = recoveredAddr
	return nil
}
