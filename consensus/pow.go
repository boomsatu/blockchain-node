package consensus

import (
	"blockchain-node/crypto"
	"blockchain-node/interfaces" // Pastikan ini diimpor
	"crypto/rand"
	"encoding/binary"
	"errors"
	// "fmt" // Tidak digunakan lagi di sini setelah menghapus fmt.Println
	"math/big"
	"time"
)

// Difficulty adjustment parameters
const (
	TargetBlockTime    = 15 * time.Second // Target 15 seconds per block
	DifficultyWindow   = 10               // Adjust difficulty every 10 blocks
	MaxDifficultyShift = 4                // Maximum 4x difficulty change
)

// ProofOfWork implements custom Proof of Work consensus algorithm
type ProofOfWork struct {
	minDifficulty *big.Int
	maxDifficulty *big.Int
}

// NewProofOfWork creates a new PoW consensus engine
func NewProofOfWork() *ProofOfWork {
	return &ProofOfWork{
		minDifficulty: big.NewInt(1000),
		maxDifficulty: new(big.Int).Lsh(big.NewInt(1), 240),
	}
}

// MineBlock performs proof of work mining on a block
// Menggunakan interfaces.BlockConsensusItf
func (pow *ProofOfWork) MineBlock(block interfaces.BlockConsensusItf) error {
	header := block.GetHeader() // Ini akan mengembalikan interfaces.BlockHeaderConsensusItf
	target := pow.calculateTarget(header.GetDifficulty())

	randomBytes := make([]byte, 8)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return errors.New("failed to generate random bytes for nonce")
	}
	header.SetNonce(binary.BigEndian.Uint64(randomBytes))

	startTime := time.Now()
	hashCount := uint64(0)

	for {
		hash := block.CalculateHash()
		hashCount++

		hashInt := new(big.Int).SetBytes(hash[:])
		if hashInt.Cmp(target) <= 0 {
			header.SetHash(hash)
			return nil
		}

		header.SetNonce(header.GetNonce() + 1)

		if hashCount%100000 == 0 {
			elapsed := time.Since(startTime)
			if elapsed > 5*time.Minute {
				return ErrMiningTimeout
			}
		}
	}
}

// ValidateProofOfWork validates the proof of work for a block
// Menggunakan interfaces.BlockConsensusItf
func (pow *ProofOfWork) ValidateProofOfWork(block interfaces.BlockConsensusItf) bool {
	header := block.GetHeader() // Ini interfaces.BlockHeaderConsensusItf

	// Recalculate block hash menggunakan metode dari interface
	// (diasumsikan block.CalculateHash() sudah benar)
	// hash := block.CalculateHash()

	// Verifikasi hash yang ada di header dengan yang dihitung ulang
	// Ini mungkin duplikat jika CalculateHash() sudah memperhitungkan nonce.
	// Biasanya, ValidateProofOfWork akan menghitung hash berdasarkan header (termasuk nonce)
	// dan membandingkannya dengan target. Hash yang tersimpan di header.GetHash()
	// seharusnya adalah hash yang valid.
	
	// Jadi, kita verifikasi bahwa hash yang tersimpan di header memenuhi target.
	currentHash := header.GetHash()
	if (currentHash == [32]byte{}) { // Jika hash belum di-set, tidak valid
		return false
	}


	target := pow.calculateTarget(header.GetDifficulty())
	hashInt := new(big.Int).SetBytes(currentHash[:])

	return hashInt.Cmp(target) <= 0
}

// CalculateDifficulty calculates the difficulty for the next block
// Menggunakan interfaces.BlockConsensusItf
func (pow *ProofOfWork) CalculateDifficulty(currentBlock interfaces.BlockConsensusItf, parentBlock interfaces.BlockConsensusItf) *big.Int {
	currentHeader := currentBlock.GetHeader()
	parentHeader := parentBlock.GetHeader()

	if currentHeader.GetNumber() < DifficultyWindow {
		return new(big.Int).Set(pow.minDifficulty)
	}

	// Waktu aktual antar blok (parent ke current)
	actualTimeForBlock := currentHeader.GetTimestamp() - parentHeader.GetTimestamp()
	
	// Untuk menyesuaikan berdasarkan window, kita perlu timestamp dari blok N-DifficultyWindow
	// Ini memerlukan akses ke blok-blok sebelumnya, yang mungkin tidak dimiliki oleh interface Engine secara langsung.
	// Untuk sementara, kita akan menyederhanakan penyesuaian berdasarkan waktu blok terakhir saja.
	// Implementasi yang lebih baik akan melihat rata-rata waktu blok dalam window.
	
	currentDifficulty := new(big.Int).Set(currentHeader.GetDifficulty()) // Atau parentHeader.GetDifficulty() tergantung kapan ini dipanggil

	// Contoh penyesuaian sederhana:
	if actualTimeForBlock < int64(TargetBlockTime.Seconds()/2) { // Terlalu cepat
		adjustment := new(big.Int).Div(currentDifficulty, big.NewInt(MaxDifficultyShift))
		if adjustment.Sign() == 0 { adjustment = big.NewInt(1) } // Pastikan ada perubahan
		currentDifficulty.Add(currentDifficulty, adjustment)
	} else if actualTimeForBlock > int64(TargetBlockTime.Seconds()*2) { // Terlalu lambat
		adjustment := new(big.Int).Div(currentDifficulty, big.NewInt(MaxDifficultyShift))
		if adjustment.Sign() == 0 { adjustment = big.NewInt(1) }
		currentDifficulty.Sub(currentDifficulty, adjustment)
	}


	if currentDifficulty.Cmp(pow.minDifficulty) < 0 {
		currentDifficulty.Set(pow.minDifficulty)
	}
	if currentDifficulty.Cmp(pow.maxDifficulty) > 0 {
		currentDifficulty.Set(pow.maxDifficulty)
	}

	return currentDifficulty
}

func (pow *ProofOfWork) calculateTarget(difficulty *big.Int) *big.Int {
	if difficulty == nil || difficulty.Sign() <= 0 { // Handle jika difficulty invalid
        // Kembalikan target yang sangat sulit (atau panic, tergantung kebijakan error)
        return new(big.Int).SetInt64(1) 
    }
	target := new(big.Int).Div(crypto.MaxTarget, difficulty)
	return target
}

var (
	ErrMiningTimeout = errors.New("mining timeout exceeded")
	// ErrInvalidProof  = errors.New("invalid proof of work") // Tidak digunakan lagi secara eksplisit
)
