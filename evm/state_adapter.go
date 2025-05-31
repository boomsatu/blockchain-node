package evm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	// vm "github.com/ethereum/go-ethereum/core/vm" // Interface vm.StateDB diimplementasikan

	customState "blockchain-node/state"
	// "blockchain-node/crypto" // Tidak digunakan langsung di sini
)

type StateAdapter struct {
	sdb *customState.StateDB
}

func NewStateAdapter(sdb *customState.StateDB) *StateAdapter {
	return &StateAdapter{sdb: sdb}
}

// Metode yang sudah ada...
func (s *StateAdapter) CreateAccount(addr common.Address) {
	s.sdb.GetAccount([20]byte(addr)) // Membuat akun jika belum ada
}
func (s *StateAdapter) SubBalance(addr common.Address, amount *big.Int) {
	s.sdb.SubBalance([20]byte(addr), amount)
}
func (s *StateAdapter) AddBalance(addr common.Address, amount *big.Int) {
	s.sdb.AddBalance([20]byte(addr), amount)
}
func (s *StateAdapter) GetBalance(addr common.Address) *big.Int {
	return s.sdb.GetBalance([20]byte(addr))
}
func (s *StateAdapter) GetNonce(addr common.Address) uint64 {
	return s.sdb.GetNonce([20]byte(addr))
}
func (s *StateAdapter) SetNonce(addr common.Address, nonce uint64) {
	s.sdb.SetNonce([20]byte(addr), nonce)
}
func (s *StateAdapter) GetCodeHash(addr common.Address) common.Hash {
	acc := s.sdb.GetAccount([20]byte(addr))
	return common.BytesToHash(acc.CodeHash[:])
}
func (s *StateAdapter) GetCode(addr common.Address) []byte {
	return s.sdb.GetCode([20]byte(addr))
}
func (s *StateAdapter) SetCode(addr common.Address, code []byte) {
	s.sdb.SetCode([20]byte(addr), code)
}
func (s *StateAdapter) GetCodeSize(addr common.Address) int {
	return len(s.sdb.GetCode([20]byte(addr)))
}
func (s *StateAdapter) AddRefund(gas uint64) { /* TODO: Implement jika customState mendukung */ }
func (s *StateAdapter) SubRefund(gas uint64) { /* TODO: Implement jika customState mendukung */ }
func (s *StateAdapter) GetRefund() uint64    { return 0 }
func (s *StateAdapter) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	val := s.sdb.GetState([20]byte(addr), [32]byte(hash))
	return common.BytesToHash(val[:])
}
func (s *StateAdapter) GetState(addr common.Address, hash common.Hash) common.Hash {
	val := s.sdb.GetState([20]byte(addr), [32]byte(hash))
	return common.BytesToHash(val[:])
}
func (s *StateAdapter) SetState(addr common.Address, key common.Hash, value common.Hash) {
	s.sdb.SetState([20]byte(addr), [32]byte(key), [32]byte(value))
}

// Metode yang hilang untuk vm.StateDB (dan CanTransfer/Transfer)
func (s *StateAdapter) Suicide(addr common.Address) bool {
	// TODO: Implementasi logika suicide di customState.StateDB jika diperlukan
	// Misalnya: s.sdb.MarkForSuicide([20]byte(addr))
	// Kembalikan true jika akun ada dan berhasil di-mark untuk suicide
	// Untuk sekarang, kembalikan false
	s.sdb.SetBalance([20]byte(addr), big.NewInt(0)) // Contoh sederhana: nolkan saldo
	// Anda juga mungkin perlu menghapus storage, code, dll.
	// Dan mentransfer sisa saldo ke penerima (biasanya coinbase atau alamat lain)
	return true // Asumsi berhasil jika ada
}

// HasSuicided diganti namanya menjadi SelfDestructed di versi go-ethereum yang lebih baru.
// Periksa interface vm.StateDB Anda untuk nama yang benar.
// Jika vm.StateDB mengharapkan HasSelfDestructed:
func (s *StateAdapter) HasSelfDestructed(addr common.Address) bool {
	// TODO: Implementasi pelacakan suicide di customState.StateDB
	return false // Placeholder
}

// Jika vm.StateDB mengharapkan SelfDestructed (lebih baru):
// func (s *StateAdapter) SelfDestructed(addr common.Address) bool {
// 	return false // Placeholder
// }

func (s *StateAdapter) Exist(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	// Akun dianggap ada jika sudah pernah berinteraksi (nonce > 0) atau punya balance atau code
	return acc.Nonce > 0 || (acc.Balance != nil && acc.Balance.Sign() > 0) || len(s.sdb.GetCode([20]byte(addr))) > 0
}

func (s *StateAdapter) Empty(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	// Akun dianggap kosong jika tidak memiliki balance, nonce, dan code.
	// Perhatikan bahwa CodeHash kosong ([32]byte{}) juga menandakan tidak ada code.
	hasCode := acc.CodeHash != ([32]byte{}) || len(s.sdb.GetCode([20]byte(addr))) > 0
	return acc.Nonce == 0 && (acc.Balance == nil || acc.Balance.Sign() == 0) && !hasCode
}

func (s *StateAdapter) PrepareAccessList(sender common.Address, dest *common.Address, precompiles []common.Address, txAccesses []ethTypes.AccessTuple) {
}
func (s *StateAdapter) AddressInAccessList(addr common.Address) bool {
	return true // Placeholder agar tidak menyebabkan error gas karena akses di luar list
}
func (s *StateAdapter) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return true, true // Placeholder
}
func (s *StateAdapter) AddAddressToAccessList(addr common.Address)                {}
func (s *StateAdapter) AddSlotToAccessList(addr common.Address, slot common.Hash) {}
func (s *StateAdapter) RevertToSnapshot(id int) {
	s.sdb.RevertToSnapshot(id)
}
func (s *StateAdapter) Snapshot() int {
	return s.sdb.Snapshot()
}
func (s *StateAdapter) AddLog(log *ethTypes.Log) {
	coreLog := &customState.Log{ // Menggunakan customState.Log
		Address:     [20]byte(log.Address),
		Topics:      make([][32]byte, len(log.Topics)),
		Data:        log.Data,
		BlockNumber: log.BlockNumber, // Ini akan diisi oleh EVM, pastikan sesuai
		TxHash:      [32]byte(log.TxHash),
		TxIndex:     uint64(log.TxIndex), // Cast uint to uint64
		BlockHash:   [32]byte(log.BlockHash),
		Index:       uint64(log.Index), // Cast uint to uint64
		Removed:     log.Removed,
	}
	for i, topic := range log.Topics {
		coreLog.Topics[i] = [32]byte(topic)
	}
	s.sdb.AddLog(coreLog)
}
func (s *StateAdapter) AddPreimage(hash common.Hash, preimage []byte) {
	// TODO: Implement jika customState mendukung penyimpanan preimages
}
func (s *StateAdapter) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	// TODO: Implement iterasi storage jika customState mendukung
	return nil // Placeholder
}

// Metode CanTransfer dan Transfer untuk vm.BlockContext
func (s *StateAdapter) CanTransfer(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	// 'db' di sini adalah interface vm.StateDB, yang dalam kasus ini adalah 's' sendiri.
	// Jadi, kita bisa menggunakan 's' atau type-assert 'db' ke '*StateAdapter'.
	// Lebih aman menggunakan 's' jika kita yakin ini selalu dipanggil pada instance StateAdapter kita.
	return s.GetBalance(addr).Cmp(amount) >= 0
}

func (s *StateAdapter) Transfer(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	s.SubBalance(sender, amount)
	s.AddBalance(recipient, amount)
}

// Implementasi metode yang mungkin masih hilang dari vm.StateDB
// (Periksa definisi interface vm.StateDB di go-ethereum untuk daftar lengkap)
// Misalnya:
// func (s *StateAdapter) Error() error { return nil } // Jika diperlukan
// func (s *StateAdapter)setError(err error) { } // Jika diperlukan

// Pastikan semua metode dari interface vm.StateDB (termasuk yang mungkin belum tercantum di sini)
// sudah diimplementasikan. Beberapa metode mungkin bisa menjadi no-op atau panic jika tidak relevan.
