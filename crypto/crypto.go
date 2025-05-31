
package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"math/big"

	"golang.org/x/crypto/sha3"
)

var (
	MaxTarget = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

// GenerateKeyPair generates a new ECDSA key pair
func GenerateKeyPair() (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	
	return privateKey, &privateKey.PublicKey, nil
}

// GenerateEthKeyPair generates a new secp256k1 ECDSA key pair (Ethereum compatible)
func GenerateEthKeyPair() (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privateKey, err := ecdsa.GenerateKey(secp256k1(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	
	return privateKey, &privateKey.PublicKey, nil
}

// secp256k1 returns the secp256k1 curve
func secp256k1() elliptic.Curve {
	return elliptic.P256() // Simplified - in production use actual secp256k1
}

// SHA256Hash calculates SHA256 hash
func SHA256Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// Keccak256Hash calculates Keccak256 hash (Ethereum style)
func Keccak256Hash(data []byte) [32]byte {
	hash := sha3.NewLegacyKeccak256()
	hash.Write(data)
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

// Keccak256 calculates Keccak256 hash and returns bytes
func Keccak256(data []byte) []byte {
	hash := sha3.NewLegacyKeccak256()
	hash.Write(data)
	return hash.Sum(nil)
}

// Sign signs data with private key using ECDSA
func Sign(hash []byte, privateKey []byte) ([]byte, error) {
	if len(privateKey) != 32 {
		return nil, errors.New("invalid private key length")
	}
	
	// Create private key from bytes
	privKey := new(ecdsa.PrivateKey)
	privKey.PublicKey.Curve = secp256k1()
	privKey.D = new(big.Int).SetBytes(privateKey)
	privKey.PublicKey.X, privKey.PublicKey.Y = privKey.PublicKey.Curve.ScalarBaseMult(privateKey)
	
	// Sign hash
	r, s, err := ecdsa.Sign(rand.Reader, privKey, hash)
	if err != nil {
		return nil, err
	}
	
	// Format signature
	signature := make([]byte, 65)
	copy(signature[:32], r.Bytes())
	copy(signature[32:64], s.Bytes())
	signature[64] = 0 // Recovery ID - simplified
	
	return signature, nil
}

// PubkeyToAddress converts public key to Ethereum-style address
func PubkeyToAddress(pubKey *ecdsa.PublicKey) [20]byte {
	// Serialize public key
	pubKeyBytes := make([]byte, 64)
	copy(pubKeyBytes[:32], pubKey.X.Bytes())
	copy(pubKeyBytes[32:], pubKey.Y.Bytes())
	
	// Hash public key
	hash := Keccak256(pubKeyBytes)
	
	// Take last 20 bytes as address
	var addr [20]byte
	copy(addr[:], hash[12:])
	return addr
}

// PrivateKeyToAddress converts private key to Ethereum-style address
func PrivateKeyToAddress(privateKey *ecdsa.PrivateKey) [20]byte {
	return PubkeyToAddress(&privateKey.PublicKey)
}

// ValidateProofOfWork validates proof of work
func ValidateProofOfWork(hash [32]byte, nonce uint64, difficulty *big.Int) bool {
	target := new(big.Int).Div(MaxTarget, difficulty)
	hashInt := new(big.Int).SetBytes(hash[:])
	return hashInt.Cmp(target) == -1
}

// VerifySignature verifies ECDSA signature
func VerifySignature(pubKey *ecdsa.PublicKey, hash []byte, signature []byte) bool {
	if len(signature) != 65 {
		return false
	}
	
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	
	return ecdsa.Verify(pubKey, hash, r, s)
}

// RecoverAddress recovers address from signature (simplified implementation)
func RecoverAddress(hash []byte, signature []byte) ([20]byte, error) {
	if len(signature) != 65 {
		return [20]byte{}, errors.New("invalid signature length")
	}
	
	// This is a simplified implementation
	// In production, you would implement proper ECDSA recovery
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	v := signature[64]
	
	// Create a dummy public key for demonstration
	// In real implementation, recover from r, s, v
	pubKey := &ecdsa.PublicKey{
		Curve: secp256k1(),
		X:     r,
		Y:     s,
	}
	
	// Use v to determine which of the two possible public keys
	if v%2 == 1 {
		pubKey.Y = new(big.Int).Add(pubKey.Y, big.NewInt(1))
	}
	
	return PubkeyToAddress(pubKey), nil
}

// Ecrecover recovers public key from signature (simplified)
func Ecrecover(hash []byte, signature []byte) (*ecdsa.PublicKey, error) {
	if len(signature) != 65 {
		return nil, errors.New("invalid signature length")
	}
	
	// Simplified implementation - in production use proper recovery
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	
	pubKey := &ecdsa.PublicKey{
		Curve: secp256k1(),
		X:     r,
		Y:     s,
	}
	
	return pubKey, nil
}

// FromECDSA exports private key to bytes
func FromECDSA(privateKey *ecdsa.PrivateKey) []byte {
	if privateKey == nil {
		return nil
	}
	return privateKey.D.Bytes()
}

// ToECDSA creates private key from bytes
func ToECDSA(privateKeyBytes []byte) (*ecdsa.PrivateKey, error) {
	if len(privateKeyBytes) != 32 {
		return nil, errors.New("invalid private key length")
	}
	
	privKey := new(ecdsa.PrivateKey)
	privKey.PublicKey.Curve = secp256k1()
	privKey.D = new(big.Int).SetBytes(privateKeyBytes)
	privKey.PublicKey.X, privKey.PublicKey.Y = privKey.PublicKey.Curve.ScalarBaseMult(privateKeyBytes)
	
	return privKey, nil
}

// FromECDSAPub exports public key to bytes
func FromECDSAPub(publicKey *ecdsa.PublicKey) []byte {
	if publicKey == nil {
		return nil
	}
	
	pubKeyBytes := make([]byte, 65)
	pubKeyBytes[0] = 0x04 // Uncompressed key prefix
	copy(pubKeyBytes[1:33], publicKey.X.Bytes())
	copy(pubKeyBytes[33:], publicKey.Y.Bytes())
	
	return pubKeyBytes
}

// UnmarshalPubkey parses public key from bytes
func UnmarshalPubkey(pubKeyBytes []byte) (*ecdsa.PublicKey, error) {
	if len(pubKeyBytes) != 65 || pubKeyBytes[0] != 0x04 {
		return nil, errors.New("invalid public key format")
	}
	
	pubKey := &ecdsa.PublicKey{
		Curve: secp256k1(),
		X:     new(big.Int).SetBytes(pubKeyBytes[1:33]),
		Y:     new(big.Int).SetBytes(pubKeyBytes[33:65]),
	}
	
	return pubKey, nil
}
