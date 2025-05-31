package state

import (
	"blockchain-node/crypto"
	"blockchain-node/database"
	"blockchain-node/trie" // Asumsi path ini benar
	"encoding/json"
	"fmt"
	"math/big"
	// "blockchain-node/logger" // Jika Anda menggunakan logger
)

// Account struct (asumsi definisi ini ada di file Anda)
type Account struct {
	Nonce    uint64   `json:"nonce"`
	Balance  *big.Int `json:"balance"`
	CodeHash [32]byte `json:"codeHash"`
	Root     [32]byte `json:"storageRoot"` // Storage trie root
}

// Log struct (asumsi definisi ini ada di file Anda atau di core dan diimpor)
type Log struct {
	Address     [20]byte   `json:"address"`
	Topics      [][32]byte `json:"topics"`
	Data        []byte     `json:"data"`
	BlockNumber uint64     `json:"blockNumber"`
	TxHash      [32]byte   `json:"transactionHash"`
	TxIndex     uint64     `json:"transactionIndex"`
	BlockHash   [32]byte   `json:"blockHash"`
	Index       uint64     `json:"logIndex"`
	Removed     bool       `json:"removed"`
}

// StateDB struct
type StateDB struct {
	db        database.Database
	trie      *trie.Trie
	accounts  map[[20]byte]*Account
	codes     map[[32]byte][]byte
	storage   map[[20]byte]map[[32]byte][32]byte // Cache untuk storage
	logs      []*Log                             // Log yang dihasilkan selama eksekusi blok
	snapshots []*StateSnapshot                   // Untuk revert
	dirty     map[[20]byte]bool                  // Akun yang dimodifikasi
	// Tambahkan field lain yang mungkin ada
}

// StateSnapshot
type StateSnapshot struct {
	accounts map[[20]byte]*Account
	codes    map[[32]byte][]byte
	storage  map[[20]byte]map[[32]byte][32]byte
	// Tambahkan field lain jika perlu
}

// NewStateDB
func NewStateDB(root [32]byte, db database.Database) (*StateDB, error) {
	stateTrie, err := trie.NewTrie(root, db)
	if err != nil {
		return nil, err
	}
	return &StateDB{
		db:       db,
		trie:     stateTrie,
		accounts: make(map[[20]byte]*Account),
		codes:    make(map[[32]byte][]byte),
		storage:  make(map[[20]byte]map[[32]byte][32]byte),
		logs:     make([]*Log, 0),
		dirty:    make(map[[20]byte]bool),
	}, nil
}

// GetAccount adalah fungsi yang diperbaiki
func (s *StateDB) GetAccount(addr [20]byte) *Account {
	if acc, exists := s.accounts[addr]; exists {
		return acc // Kembalikan pointer yang sudah ada di cache
	}

	data, err := s.trie.Get(addr[:])
	if err != nil {
		// logger.Errorf("Error getting account %x from trie: %v", addr, err)
		// Kembalikan akun baru/kosong jika error atau tidak ditemukan
		newAcc := &Account{Nonce: 0, Balance: big.NewInt(0)}
		s.accounts[addr] = newAcc // Cache akun baru ini
		return newAcc
	}
	if data == nil { // Akun tidak ada di trie
		newAcc := &Account{Nonce: 0, Balance: big.NewInt(0)}
		s.accounts[addr] = newAcc // Cache akun baru ini
		return newAcc
	}

	var acc Account                                    // Deklarasikan sebagai TIPE NILAI
	if err := json.Unmarshal(data, &acc); err != nil { // Unmarshal ke alamat dari acc (&acc)
		// logger.Errorf("Error unmarshalling account %x: %v", addr, err)
		newAcc := &Account{Nonce: 0, Balance: big.NewInt(0)}
		s.accounts[addr] = newAcc
		return newAcc
	}
	// Setelah unmarshal berhasil, acc adalah variabel lokal.
	// Kita perlu menyimpan POINTER ke data ini di map dan mengembalikannya.
	// Go escape analysis akan memindahkan `acc` ke heap jika pointernya disimpan.
	accountPtr := &acc
	s.accounts[addr] = accountPtr // Simpan pointer di cache
	return accountPtr             // Kembalikan pointer
}

func (s *StateDB) SetBalance(addr [20]byte, balance *big.Int) {
	acc := s.GetAccount(addr)
	acc.Balance = new(big.Int).Set(balance)
	s.accounts[addr] = acc
	s.dirty[addr] = true
}

func (s *StateDB) GetBalance(addr [20]byte) *big.Int {
	acc := s.GetAccount(addr)
	if acc.Balance == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(acc.Balance)
}

func (s *StateDB) SetNonce(addr [20]byte, nonce uint64) {
	acc := s.GetAccount(addr)
	acc.Nonce = nonce
	s.accounts[addr] = acc
	s.dirty[addr] = true
}

func (s *StateDB) GetNonce(addr [20]byte) uint64 {
	acc := s.GetAccount(addr)
	return acc.Nonce
}

func (s *StateDB) GetCode(addr [20]byte) []byte {
	acc := s.GetAccount(addr)
	if acc.CodeHash == ([32]byte{}) {
		return nil
	}
	if code, exists := s.codes[acc.CodeHash]; exists {
		return code
	}
	// TODO: Muat kode dari database jika tidak ada di cache s.codes
	// codeBytes, err := s.db.Get(append([]byte("code_"), acc.CodeHash[:]...))
	// if err == nil && codeBytes != nil {
	//    s.codes[acc.CodeHash] = codeBytes
	//    return codeBytes
	// }
	return nil
}

func (s *StateDB) SetCode(addr [20]byte, code []byte) {
	acc := s.GetAccount(addr)
	if len(code) == 0 {
		acc.CodeHash = [32]byte{}
	} else {
		acc.CodeHash = crypto.Keccak256Hash(code)
		s.codes[acc.CodeHash] = code
		// TODO: Simpan kode ke database
		// s.db.Put(append([]byte("code_"), acc.CodeHash[:]...), code)
	}
	s.accounts[addr] = acc
	s.dirty[addr] = true
}

func (s *StateDB) GetState(addr [20]byte, key [32]byte) [32]byte {
	if storageMap, exists := s.storage[addr]; exists {
		if val, existsVal := storageMap[key]; existsVal {
			return val
		}
	}
	acc := s.GetAccount(addr)
	if acc.Root == ([32]byte{}) {
		return [32]byte{}
	}
	storageTrie, err := trie.NewTrie(acc.Root, s.db)
	if err != nil {
		// logger.Errorf("Failed to load storage trie for account %x: %v", addr, err)
		return [32]byte{}
	}
	valBytes, err := storageTrie.Get(key[:])
	if err != nil || valBytes == nil {
		return [32]byte{}
	}
	var val [32]byte
	copy(val[:], valBytes)

	if s.storage[addr] == nil {
		s.storage[addr] = make(map[[32]byte][32]byte)
	}
	s.storage[addr][key] = val
	return val
}

func (s *StateDB) SetState(addr [20]byte, key [32]byte, value [32]byte) {
	if s.storage[addr] == nil {
		s.storage[addr] = make(map[[32]byte][32]byte)
	}
	s.storage[addr][key] = value
	s.dirty[addr] = true
}

func (s *StateDB) Commit() ([32]byte, error) {
	for addr, isDirty := range s.dirty {
		if !isDirty {
			continue
		}
		acc := s.accounts[addr]
		if acc == nil { // Seharusnya tidak terjadi jika dirty
			// logger.Warningf("Account %x marked dirty but not found in cache during commit", addr)
			continue
		}

		if storageSlots, hasStorage := s.storage[addr]; hasStorage && len(storageSlots) > 0 {
			storageTrie, err := trie.NewTrie(acc.Root, s.db)
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to load/create storage trie for %x: %v", addr, err)
			}
			for key, value := range storageSlots {
				if err := storageTrie.Update(key[:], value[:]); err != nil {
					return [32]byte{}, fmt.Errorf("failed to update storage for %x, key %x: %v", addr, key, err)
				}
			}
			newStorageRoot, err := storageTrie.Commit()
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to commit storage trie for %x: %v", addr, err)
			}
			acc.Root = newStorageRoot
		}

		accBytes, err := json.Marshal(acc)
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to marshal account %x: %v", addr, err)
		}
		if err := s.trie.Update(addr[:], accBytes); err != nil {
			return [32]byte{}, fmt.Errorf("failed to update account %x in state trie: %v", addr, err)
		}
	}

	newRoot, err := s.trie.Commit()
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to commit main state trie: %v", err)
	}

	s.dirty = make(map[[20]byte]bool)
	s.logs = []*Log{} // Reset logs setelah commit
	return newRoot, nil
}

func (s *StateDB) Snapshot() int {
	snap := &StateSnapshot{
		accounts: make(map[[20]byte]*Account),
		codes:    make(map[[32]byte][]byte),
		storage:  make(map[[20]byte]map[[32]byte][32]byte),
	}
	for addr, accPtr := range s.accounts {
		if accPtr == nil {
			continue
		}
		clonedAcc := *accPtr // Dereference untuk membuat salinan nilai
		if accPtr.Balance != nil {
			clonedAcc.Balance = new(big.Int).Set(accPtr.Balance) // Deep copy balance
		} else {
			clonedAcc.Balance = big.NewInt(0) // Inisialisasi jika nil
		}
		snap.accounts[addr] = &clonedAcc // Simpan pointer ke salinan
	}
	for hash, code := range s.codes {
		clonedCode := make([]byte, len(code))
		copy(clonedCode, code)
		snap.codes[hash] = clonedCode
	}
	for addr, storageMap := range s.storage {
		snap.storage[addr] = make(map[[32]byte][32]byte)
		for key, val := range storageMap {
			snap.storage[addr][key] = val
		}
	}
	s.snapshots = append(s.snapshots, snap)
	return len(s.snapshots) - 1
}

func (s *StateDB) RevertToSnapshot(snapID int) {
	if snapID < 0 || snapID >= len(s.snapshots) {
		return
	}
	snap := s.snapshots[snapID]

	s.accounts = make(map[[20]byte]*Account)
	for addr, accPtr := range snap.accounts {
		if accPtr == nil {
			continue
		}
		clonedAcc := *accPtr
		if accPtr.Balance != nil {
			clonedAcc.Balance = new(big.Int).Set(accPtr.Balance)
		} else {
			clonedAcc.Balance = big.NewInt(0)
		}
		s.accounts[addr] = &clonedAcc
	}
	s.codes = make(map[[32]byte][]byte)
	for hash, code := range snap.codes {
		clonedCode := make([]byte, len(code))
		copy(clonedCode, code)
		s.codes[hash] = clonedCode
	}
	s.storage = make(map[[20]byte]map[[32]byte][32]byte)
	for addr, storageMap := range snap.storage {
		s.storage[addr] = make(map[[32]byte][32]byte)
		for key, val := range storageMap {
			s.storage[addr][key] = val
		}
	}

	s.snapshots = s.snapshots[:snapID]
	s.dirty = make(map[[20]byte]bool)
	// Log juga mungkin perlu di-revert atau dibersihkan
	// Untuk simplifikasi, kita bisa bersihkan log yang ada setelah revert
	s.logs = []*Log{}
}

func (s *StateDB) AddLog(log *Log) {
	s.logs = append(s.logs, log)
}

func (s *StateDB) GetLogs(txHash [32]byte) []*Log {
	// Implementasi ini bisa mengumpulkan log berdasarkan txHash jika diperlukan,
	// atau mengembalikan semua log yang terkumpul untuk blok saat ini.
	// Untuk sekarang, asumsikan ini mengembalikan semua log yang ada di s.logs.
	// Jika Anda ingin memfilter berdasarkan txHash, Anda perlu iterasi s.logs.
	return s.logs
}

// Trie mengembalikan instance trie yang digunakan oleh StateDB.
func (s *StateDB) Trie() *trie.Trie {
	return s.trie
}

// CurrentRoot mengembalikan hash dari root node trie saat ini.
// Ini mungkin belum di-commit ke DB.
// BARU: Method untuk mengatasi error `bc.stateDB.Trie().Hash undefined`
func (s *StateDB) CurrentRoot() ([32]byte, error) {
	if s.trie == nil || s.trie.RootNode() == nil { // Tambahkan method RootNode() di trie.Trie
		// Jika trie atau root node-nya nil, kita bisa kembalikan hash kosong
		// atau hash dari state yang terakhir di-commit jika itu yang diinginkan.
		// Untuk konsistensi, jika trie belum ada, root-nya adalah kosong.
		// Jika trie ada tapi root-nya nil (setelah delete semua), itu juga bisa jadi hash kosong.
		// logger.Warning("CurrentRoot called on nil trie or nil root node, returning zero hash.")
		return [32]byte{}, nil // Atau error jika ini kondisi tak terduga
	}
	// Asumsi s.trie.RootNode().Hash adalah hash dari node root saat ini
	return s.trie.RootNode().Hash, nil
}

func (s *StateDB) SubBalance(addr [20]byte, amount *big.Int) {
	acc := s.GetAccount(addr)
	if acc.Balance == nil {
		acc.Balance = big.NewInt(0)
	}
	acc.Balance.Sub(acc.Balance, amount)
	s.accounts[addr] = acc
	s.dirty[addr] = true
}

func (s *StateDB) AddBalance(addr [20]byte, amount *big.Int) {
	acc := s.GetAccount(addr)
	if acc.Balance == nil {
		acc.Balance = big.NewInt(0)
	}
	acc.Balance.Add(acc.Balance, amount)
	s.accounts[addr] = acc
	s.dirty[addr] = true
}
