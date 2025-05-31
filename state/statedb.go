package state

import (
	"blockchain-node/crypto"
	"blockchain-node/database"
	"blockchain-node/logger"
	"blockchain-node/trie" // Asumsi path ini benar
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// Account struct
type Account struct {
	Nonce    uint64   `json:"nonce"`
	Balance  *big.Int `json:"balance"`
	CodeHash [32]byte `json:"codeHash"`
	Root     [32]byte `json:"storageRoot"` // Storage trie root
}

// Log struct (konsisten dengan interfaces.LogInternal)
type Log struct {
	Address     [20]byte   `json:"address"`
	Topics      [][32]byte `json:"topics"`
	Data        []byte     `json:"data"`
	BlockNumber uint64     `json:"blockNumber"`
	TxHash      [32]byte   `json:"transactionHash"`
	TxIndex     uint64     `json:"transactionIndex"`
	BlockHash   [32]byte   `json:"blockHash"`
	Index       uint64     `json:"logIndex"` // Indeks log dalam transaksi
	// Removed bool // 'Removed' bisa ditangani dengan tidak menyertakannya saat revert
}

// StateDB struct
type StateDB struct {
	db   database.Database
	trie *trie.Trie

	// Cache untuk objek state
	accounts map[[20]byte]*Account
	codes    map[[32]byte][]byte
	storage  map[[20]byte]map[[32]byte][32]byte // Cache untuk storage per akun

	// Log yang dihasilkan selama eksekusi blok saat ini
	// Ini adalah daftar semua log dalam blok, akan di-commit bersama blok.
	// Untuk pengambilan log per transaksi sebelum commit, kita perlu mekanisme tambahan
	// atau mengambilnya dari sini dan memfilter.
	blockLogs []*Log

	// Untuk revert per transaksi
	snapshots []*StateSnapshot
	dirty     map[[20]byte]bool // Akun yang dimodifikasi sejak snapshot terakhir atau awal blok

	mu sync.RWMutex // Mutex untuk melindungi akses ke field StateDB
}

// StateSnapshot menyimpan state yang relevan untuk revert.
type StateSnapshot struct {
	accounts         map[[20]byte]*Account              // Salinan pointer ke akun yang di-cache
	codes            map[[32]byte][]byte                // Salinan cache kode
	storage          map[[20]byte]map[[32]byte][32]byte // Salinan cache storage
	dirtyAccounts    map[[20]byte]bool                  // Salinan status dirty akun
	blockLogsCount   int                                // Jumlah log di blockLogs sebelum snapshot ini
	snapshotTrieRoot [32]byte                           // Root trie pada saat snapshot (opsional, untuk verifikasi)
}

// NewStateDB membuat instance StateDB baru.
func NewStateDB(root [32]byte, db database.Database) (*StateDB, error) {
	stateTrie, err := trie.NewTrie(root, db)
	if err != nil {
		return nil, fmt.Errorf("failed to create trie for StateDB: %w", err)
	}
	return &StateDB{
		db:        db,
		trie:      stateTrie,
		accounts:  make(map[[20]byte]*Account),
		codes:     make(map[[32]byte][]byte),
		storage:   make(map[[20]byte]map[[32]byte][32]byte),
		blockLogs: make([]*Log, 0),
		dirty:     make(map[[20]byte]bool),
		snapshots: make([]*StateSnapshot, 0),
	}, nil
}

// GetAccount mengambil akun dari cache atau memuatnya dari trie.
// Selalu mengembalikan pointer ke Account (bisa jadi akun kosong jika tidak ada).
func (s *StateDB) GetAccount(addr [20]byte) *Account {
	s.mu.RLock()
	if acc, exists := s.accounts[addr]; exists {
		s.mu.RUnlock()
		return acc
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// Periksa lagi jika sudah dimuat oleh goroutine lain
	if acc, exists := s.accounts[addr]; exists {
		return acc
	}

	data, err := s.trie.Get(addr[:])
	if err != nil {
		logger.Errorf("Error getting account %x from trie: %v", addr, err)
		// Kembalikan akun baru/kosong jika error
		newAcc := &Account{Nonce: 0, Balance: big.NewInt(0)}
		s.accounts[addr] = newAcc
		return newAcc
	}
	if data == nil { // Akun tidak ada di trie
		newAcc := &Account{Nonce: 0, Balance: big.NewInt(0)}
		s.accounts[addr] = newAcc
		return newAcc
	}

	var acc Account
	if err := json.Unmarshal(data, &acc); err != nil {
		logger.Errorf("Error unmarshalling account %x: %v", addr, err)
		newAcc := &Account{Nonce: 0, Balance: big.NewInt(0)}
		s.accounts[addr] = newAcc
		return newAcc
	}
	// Pastikan balance tidak nil setelah unmarshal
	if acc.Balance == nil {
		acc.Balance = big.NewInt(0)
	}

	accountPtr := &acc
	s.accounts[addr] = accountPtr
	return accountPtr
}

// updateAccount memperbarui akun di cache dan menandainya sebagai dirty.
// Metode ini harus dipanggil di dalam Lock.
func (s *StateDB) updateAccount(addr [20]byte, acc *Account) {
	s.accounts[addr] = acc
	s.dirty[addr] = true
}

func (s *StateDB) SetBalance(addr [20]byte, balance *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc := s.GetAccount(addr) // GetAccount sudah thread-safe
	// Buat salinan balance untuk menghindari modifikasi tak terduga pada pointer asli
	acc.Balance = new(big.Int).Set(balance)
	s.updateAccount(addr, acc)
}

func (s *StateDB) GetBalance(addr [20]byte) *big.Int {
	// GetAccount sudah thread-safe
	acc := s.GetAccount(addr)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if acc.Balance == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(acc.Balance) // Kembalikan salinan
}

func (s *StateDB) SetNonce(addr [20]byte, nonce uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc := s.GetAccount(addr)
	acc.Nonce = nonce
	s.updateAccount(addr, acc)
}

func (s *StateDB) GetNonce(addr [20]byte) uint64 {
	acc := s.GetAccount(addr)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return acc.Nonce
}

func (s *StateDB) GetCode(addr [20]byte) []byte {
	acc := s.GetAccount(addr)
	s.mu.RLock()
	codeHash := acc.CodeHash
	s.mu.RUnlock()

	if codeHash == ([32]byte{}) {
		return nil
	}

	s.mu.RLock()
	if code, exists := s.codes[codeHash]; exists {
		s.mu.RUnlock()
		return code
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// Periksa lagi jika sudah dimuat
	if code, exists := s.codes[codeHash]; exists {
		return code
	}

	// Muat kode dari database utama (bukan trie)
	// Diasumsikan kode disimpan dengan prefix "code_" diikuti codehash
	codeBytes, err := s.db.Get(append([]byte("code_"), codeHash[:]...))
	if err != nil {
		logger.Errorf("Failed to load code for hash %x from DB: %v", codeHash, err)
		return nil
	}
	if codeBytes != nil {
		s.codes[codeHash] = codeBytes
		return codeBytes
	}
	logger.Warningf("Code for hash %x not found in DB, though account references it.", codeHash)
	return nil
}

func (s *StateDB) SetCode(addr [20]byte, code []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc := s.GetAccount(addr)
	if len(code) == 0 {
		acc.CodeHash = [32]byte{}
	} else {
		codeHash := crypto.Keccak256Hash(code)
		acc.CodeHash = codeHash
		s.codes[codeHash] = code
		// Simpan kode ke database utama
		if err := s.db.Put(append([]byte("code_"), codeHash[:]...), code); err != nil {
			logger.Errorf("Failed to save code for hash %x to DB: %v", codeHash, err)
			// Pertimbangkan bagaimana menangani error ini, mungkin revert atau log fatal.
		}
	}
	s.updateAccount(addr, acc)
}

func (s *StateDB) GetState(addr [20]byte, key [32]byte) [32]byte {
	s.mu.RLock()
	if storageMap, exists := s.storage[addr]; exists {
		if val, existsVal := storageMap[key]; existsVal {
			s.mu.RUnlock()
			return val
		}
	}
	s.mu.RUnlock()

	// Jika tidak ada di cache, muat dari storage trie akun
	acc := s.GetAccount(addr) // Ini sudah thread-safe
	s.mu.RLock()
	accRoot := acc.Root
	s.mu.RUnlock()

	if accRoot == ([32]byte{}) {
		return [32]byte{} // Akun tidak memiliki storage trie
	}

	// Buat instance trie sementara untuk storage akun ini
	// Perhatian: Ini bisa mahal jika sering dilakukan. Cache sangat penting.
	storageTrie, err := trie.NewTrie(accRoot, s.db)
	if err != nil {
		logger.Errorf("Failed to load storage trie for account %x (root %x): %v", addr, accRoot, err)
		return [32]byte{}
	}

	valBytes, err := storageTrie.Get(key[:])
	if err != nil || valBytes == nil {
		// logger.Debugf("Key %x not found in storage trie for account %x", key, addr)
		return [32]byte{}
	}
	var val [32]byte
	copy(val[:], valBytes)

	// Simpan ke cache
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storage[addr] == nil {
		s.storage[addr] = make(map[[32]byte][32]byte)
	}
	s.storage[addr][key] = val
	return val
}

func (s *StateDB) SetState(addr [20]byte, key [32]byte, value [32]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storage[addr] == nil {
		s.storage[addr] = make(map[[32]byte][32]byte)
	}
	s.storage[addr][key] = value
	s.dirty[addr] = true // Akun menjadi dirty karena storage-nya berubah
}

// Commit menulis semua perubahan state (akun dan storage) ke trie utama dan storage tries,
// lalu mengembalikan root hash baru dari state trie utama.
func (s *StateDB) Commit() ([32]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for addr, isDirty := range s.dirty {
		if !isDirty {
			continue
		}
		acc, accExists := s.accounts[addr]
		if !accExists || acc == nil {
			logger.Warningf("Account %x marked dirty but not found in cache during commit", addr)
			continue
		}

		// Commit storage trie untuk akun ini jika ada perubahan storage
		if storageSlots, hasStorageChanges := s.storage[addr]; hasStorageChanges && len(storageSlots) > 0 {
			// Muat atau buat storage trie untuk akun ini
			// acc.Root adalah root SEBELUM perubahan di blok ini.
			// Kita perlu membuat storageTrie dari root itu, terapkan perubahan, lalu commit.
			storageTrie, err := trie.NewTrie(acc.Root, s.db) // Gunakan root lama sebagai basis
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to load/create storage trie for %x (root %x): %w", addr, acc.Root, err)
			}
			for key, value := range storageSlots { // Terapkan perubahan dari cache storage
				if err := storageTrie.Update(key[:], value[:]); err != nil {
					return [32]byte{}, fmt.Errorf("failed to update storage for %x, key %x: %w", addr, key, err)
				}
			}
			newStorageRoot, err := storageTrie.Commit()
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to commit storage trie for %x: %w", addr, err)
			}
			acc.Root = newStorageRoot // Update root storage di objek akun
			logger.Debugf("Committed storage trie for account %x, new root: %x", addr, newStorageRoot)
		}

		// Serialize akun dan update di state trie utama
		accBytes, err := json.Marshal(acc)
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to marshal account %x: %w", addr, err)
		}
		if err := s.trie.Update(addr[:], accBytes); err != nil {
			return [32]byte{}, fmt.Errorf("failed to update account %x in state trie: %w", addr, err)
		}
	}

	newRoot, err := s.trie.Commit()
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to commit main state trie: %w", err)
	}

	// Reset status dirty dan cache storage setelah commit berhasil
	s.dirty = make(map[[20]byte]bool)
	s.storage = make(map[[20]byte]map[[32]byte][32]byte) // Kosongkan cache storage
	// blockLogs di-reset oleh Blockchain setelah blok di-commit

	logger.Debugf("Main state trie committed, new root: %x", newRoot)
	return newRoot, nil
}

// Snapshot membuat snapshot dari state saat ini.
func (s *StateDB) Snapshot() int {
	s.mu.Lock() // Perlu Lock karena kita memodifikasi s.snapshots dan membaca banyak state
	defer s.mu.Unlock()

	// Salin state yang relevan. Penting untuk melakukan deep copy jika memungkinkan
	// atau setidaknya menyadari bahwa ini adalah copy dari pointer untuk map.
	copiedAccounts := make(map[[20]byte]*Account)
	for addr, accPtr := range s.accounts {
		if accPtr != nil {
			// Salin struct Account, bukan hanya pointer
			clonedAcc := *accPtr
			if accPtr.Balance != nil {
				clonedAcc.Balance = new(big.Int).Set(accPtr.Balance) // Deep copy balance
			} else {
				clonedAcc.Balance = big.NewInt(0)
			}
			copiedAccounts[addr] = &clonedAcc
		}
	}

	copiedCodes := make(map[[32]byte][]byte)
	for hash, code := range s.codes {
		clonedCode := make([]byte, len(code))
		copy(clonedCode, code)
		copiedCodes[hash] = clonedCode
	}

	copiedStorage := make(map[[20]byte]map[[32]byte][32]byte)
	for addr, storageMap := range s.storage {
		copiedStorage[addr] = make(map[[32]byte][32]byte)
		for key, val := range storageMap {
			copiedStorage[addr][key] = val // [32]byte adalah array, jadi ini adalah copy
		}
	}

	copiedDirty := make(map[[20]byte]bool)
	for addr, d := range s.dirty {
		copiedDirty[addr] = d
	}

	currentTrieRootNode := s.trie.RootNode()
	var currentTrieRootHash [32]byte
	if currentTrieRootNode != nil {
		currentTrieRootHash = currentTrieRootNode.Hash // Asumsi Hash sudah di-commit atau valid
	}

	snap := &StateSnapshot{
		accounts:         copiedAccounts,
		codes:            copiedCodes,
		storage:          copiedStorage,
		dirtyAccounts:    copiedDirty,
		blockLogsCount:   len(s.blockLogs),
		snapshotTrieRoot: currentTrieRootHash,
	}
	s.snapshots = append(s.snapshots, snap)
	return len(s.snapshots) - 1
}

// RevertToSnapshot mengembalikan state ke snapshot yang diberikan.
func (s *StateDB) RevertToSnapshot(snapID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snapID < 0 || snapID >= len(s.snapshots) {
		logger.Errorf("Invalid snapshot ID %d for revert, current snapshot count %d", snapID, len(s.snapshots))
		return
	}
	snap := s.snapshots[snapID]

	// Restore state dari snapshot (ini adalah salinan, jadi aman untuk di-assign)
	s.accounts = snap.accounts
	s.codes = snap.codes
	s.storage = snap.storage
	s.dirty = snap.dirtyAccounts

	// Revert blockLogs
	if snap.blockLogsCount < len(s.blockLogs) {
		s.blockLogs = s.blockLogs[:snap.blockLogsCount]
	}

	// Revert trie ke root pada saat snapshot (jika root disimpan dan berbeda)
	// Ini adalah bagian yang rumit. Jika trie.RevertToRoot() tidak ada,
	// Anda mungkin perlu membuat instance trie baru dari snap.snapshotTrieRoot.
	// Untuk sekarang, kita asumsikan perubahan trie akan dibatalkan karena
	// akun yang dimodifikasi setelah snapshot akan di-revert dari cache.
	// Saat Commit berikutnya, hanya akun yang masih dirty (dari sebelum snapshot atau
	// dimodifikasi lagi setelah revert) yang akan ditulis ke trie.
	// Jika snap.snapshotTrieRoot valid, kita bisa reset trie:
	if snap.snapshotTrieRoot != ([32]byte{}) {
		newTrie, err := trie.NewTrie(snap.snapshotTrieRoot, s.db)
		if err != nil {
			logger.Fatalf("CRITICAL: Failed to revert trie to snapshot root %x: %v. StateDB may be inconsistent.", snap.snapshotTrieRoot, err)
			// Ini adalah kondisi error serius.
		} else {
			s.trie = newTrie
			logger.Debugf("Reverted main state trie to root %x from snapshot %d", snap.snapshotTrieRoot, snapID)
		}
	} else if snapID == 0 && len(s.snapshots) > 0 { // Jika revert ke snapshot paling awal (mungkin sebelum genesis)
		// Untuk snapshot paling awal, root bisa jadi kosong.
		newTrie, err := trie.NewTrie([32]byte{}, s.db)
		if err != nil {
			logger.Fatalf("CRITICAL: Failed to revert trie to empty root for initial snapshot: %v", err)
		} else {
			s.trie = newTrie
			logger.Debugf("Reverted main state trie to empty root from initial snapshot %d", snapID)
		}
	}

	// Hapus snapshot ini dan semua setelahnya
	s.snapshots = s.snapshots[:snapID]
	logger.Debugf("Reverted state to snapshot ID %d. Current snapshots: %d", snapID, len(s.snapshots))
}

// AddLog menambahkan log ke daftar log blok saat ini.
func (s *StateDB) AddLog(log *Log) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Atur indeks log dalam blok (jika belum diatur oleh pemanggil)
	// log.Index = uint64(len(s.blockLogs)) // Ini lebih cocok diatur oleh core.Blockchain saat membuat receipt
	s.blockLogs = append(s.blockLogs, log)
}

// GetLogs (BARU): Mengambil log yang cocok dengan txHash dari blockLogs saat ini.
// Ini adalah implementasi sederhana; untuk produksi, pertimbangkan indeks.
func (s *StateDB) GetLogs(txHash [32]byte) []*Log {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var matchedLogs []*Log
	for _, log := range s.blockLogs { // Iterasi dari s.blockLogs (log untuk blok saat ini)
		if log.TxHash == txHash {
			// Buat salinan log untuk menghindari modifikasi tak terduga
			logCopy := *log
			logCopy.Topics = make([][32]byte, len(log.Topics))
			for i, t := range log.Topics {
				logCopy.Topics[i] = t
			}
			logCopy.Data = common.CopyBytes(log.Data)
			matchedLogs = append(matchedLogs, &logCopy)
		}
	}
	return matchedLogs
}

// GetAllBlockLogs mengembalikan semua log yang terkumpul untuk blok saat ini.
// Dipanggil oleh core.Blockchain setelah semua transaksi dieksekusi.
func (s *StateDB) GetAllBlockLogs() []*Log {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Kembalikan salinan untuk mencegah modifikasi eksternal
	logsCopy := make([]*Log, len(s.blockLogs))
	for i, l := range s.blockLogs {
		logCopy := *l // Salin struct Log
		// Salin slice secara mendalam jika diperlukan (Topics, Data)
		logCopy.Topics = make([][32]byte, len(l.Topics))
		for j, t := range l.Topics {
			logCopy.Topics[j] = t
		}
		logCopy.Data = common.CopyBytes(l.Data)
		logsCopy[i] = &logCopy
	}
	return logsCopy
}

// ClearBlockLogs membersihkan log blok, biasanya dipanggil setelah blok di-commit.
func (s *StateDB) ClearBlockLogs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockLogs = make([]*Log, 0)
}

// Trie mengembalikan instance trie yang digunakan oleh StateDB.
func (s *StateDB) Trie() *trie.Trie {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.trie
}

// CurrentRoot mengembalikan hash dari root node trie saat ini (mungkin belum di-commit).
func (s *StateDB) CurrentRoot() ([32]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.trie == nil || s.trie.RootNode() == nil {
		return [32]byte{}, nil // Hash kosong jika trie atau root-nya nil
	}
	// Jika RootNode().Hash belum di-commit, ini mungkin hash lama.
	// Untuk mendapatkan root "kotor" saat ini, kita mungkin perlu Commit sementara (dry run)
	// atau trie perlu mengekspos metode untuk menghitung hash kotor.
	// Untuk sekarang, kita asumsikan RootNode().Hash adalah yang terbaik yang kita punya tanpa commit.
	// Atau, jika kita tahu stateDB akan di-commit, kita bisa panggil s.trie.Commit() di sini
	// tapi itu akan menulis ke DB.
	// Solusi yang lebih baik adalah trie memiliki metode `DirtyRootHash()`
	return s.trie.RootNode().Hash, nil // Ini adalah hash dari root yang terakhir di-commit oleh trie.
}

func (s *StateDB) SubBalance(addr [20]byte, amount *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc := s.GetAccount(addr)
	if acc.Balance == nil {
		acc.Balance = big.NewInt(0)
	}
	acc.Balance.Sub(acc.Balance, amount)
	s.updateAccount(addr, acc)
}

func (s *StateDB) AddBalance(addr [20]byte, amount *big.Int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc := s.GetAccount(addr)
	if acc.Balance == nil {
		acc.Balance = big.NewInt(0)
	}
	acc.Balance.Add(acc.Balance, amount)
	s.updateAccount(addr, acc)
}

// ClearStorage (BARU): Metode hipotetis untuk menghapus semua storage untuk sebuah akun.
// Ini diperlukan untuk implementasi Suicide yang benar.
func (s *StateDB) ClearStorage(addr [20]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc := s.GetAccount(addr)
	if acc.Root == ([32]byte{}) {
		// Tidak ada storage trie, tidak ada yang perlu dihapus.
		return
	}

	// Hapus semua entri dari cache storage untuk akun ini
	delete(s.storage, addr)

	// Untuk menghapus dari trie, idealnya trie memiliki metode Clear() atau
	// kita perlu iterasi dan hapus semua kunci.
	// Atau, cukup set acc.Root ke hash kosong.
	// Saat commit berikutnya, jika tidak ada SetState, trie lama akan "terlupakan".
	// Namun, untuk kebersihan, lebih baik menghapus node trie jika memungkinkan.
	// Untuk simplifikasi, kita set root ke kosong.
	acc.Root = [32]byte{}
	s.updateAccount(addr, acc)
	logger.Debugf("Cleared storage (set root to empty) for account %x", addr)
}
