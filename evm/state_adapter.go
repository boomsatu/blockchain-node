package evm

import (
	"blockchain-node/logger" // Pastikan logger diimpor jika digunakan
	customState "blockchain-node/state"
	"math/big"

	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"   // Digunakan untuk state.AccessEvents
	"github.com/ethereum/go-ethereum/core/tracing" // Diperlukan untuk BalanceChangeReason
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm" // Pastikan vm.ContractRef dan vm.NewContractRef ada di sini
	"github.com/holiman/uint256" // Untuk tipe uint256.Int
)

// StateAdapter menjembatani customState.StateDB Anda dengan vm.StateDB yang dibutuhkan oleh go-ethereum.
type StateAdapter struct {
	sdb *customState.StateDB
}

// NewStateAdapter membuat instance StateAdapter baru.
func NewStateAdapter(sdb *customState.StateDB) *StateAdapter {
	return &StateAdapter{sdb: sdb}
}

// Implementasi vm.StateDB

// CreateAccount memastikan akun ada. Di StateDB kustom Anda, GetAccount mungkin sudah membuatnya jika tidak ada.
func (s *StateAdapter) CreateAccount(addr common.Address) {
	_ = s.sdb.GetAccount([20]byte(addr)) // Memanggil GetAccount akan membuat akun jika belum ada.
}

// CreateContract membuat referensi kontrak baru untuk alamat yang diberikan.
// Ini adalah bagian dari interface vm.StateDB.
// Jika vm.ContractRef atau vm.NewContractRef undefined, periksa versi go-ethereum Anda.
func (s *StateAdapter) CreateContract(addr common.Address) vm.ContractRef {
	s.CreateAccount(addr) // Pastikan akun ada di stateDB kustom Anda.
	// Mengembalikan ContractRef baru.
	return vm.NewContractRef(addr)
}

// SubBalance mengurangi saldo akun dan mengembalikan saldo baru.
func (s *StateAdapter) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	amountBigInt := amount.ToBig()
	s.sdb.SubBalance([20]byte(addr), amountBigInt)
	logger.Debugf("StateAdapter: SubBalance for %s, amount %s, reason: %v", addr.Hex(), amount.String(), reason)

	newBalanceBigInt := s.sdb.GetBalance([20]byte(addr))
	newBalanceUint256, overflow := uint256.FromBig(newBalanceBigInt)
	if overflow {
		logger.Errorf("StateAdapter: SubBalance - balance overflowed uint256 for address %s", addr.Hex())
		return *uint256.NewInt(0)
	}
	if newBalanceUint256 == nil {
		return *uint256.NewInt(0)
	}
	return *newBalanceUint256
}

// AddBalance menambah saldo akun dan mengembalikan saldo baru.
func (s *StateAdapter) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	amountBigInt := amount.ToBig()
	s.sdb.AddBalance([20]byte(addr), amountBigInt)
	logger.Debugf("StateAdapter: AddBalance for %s, amount %s, reason: %v", addr.Hex(), amount.String(), reason)

	newBalanceBigInt := s.sdb.GetBalance([20]byte(addr))
	newBalanceUint256, overflow := uint256.FromBig(newBalanceBigInt)
	if overflow {
		logger.Errorf("StateAdapter: AddBalance - balance overflowed uint256 for address %s", addr.Hex())
		return *uint256.NewInt(0)
	}
	if newBalanceUint256 == nil {
		return *uint256.NewInt(0)
	}
	return *newBalanceUint256
}

// GetBalance mengambil saldo akun.
func (s *StateAdapter) GetBalance(addr common.Address) *uint256.Int {
	balanceBigInt := s.sdb.GetBalance([20]byte(addr))
	if balanceBigInt == nil {
		return uint256.NewInt(0)
	}
	balanceUint256, overflow := uint256.FromBig(balanceBigInt)
	if overflow {
		logger.Errorf("StateAdapter: GetBalance - balance overflowed uint256 for address %s", addr.Hex())
		maxVal := new(uint256.Int).SetAllOne()
		return maxVal
	}
	if balanceUint256 == nil {
		return uint256.NewInt(0)
	}
	return balanceUint256
}

// GetNonce mengambil nonce akun.
func (s *StateAdapter) GetNonce(addr common.Address) uint64 {
	return s.sdb.GetNonce([20]byte(addr))
}

// SetNonce mengatur nonce akun.
func (s *StateAdapter) SetNonce(addr common.Address, nonce uint64) {
	s.sdb.SetNonce([20]byte(addr), nonce)
}

// GetCodeHash mengambil hash dari kode kontrak akun.
func (s *StateAdapter) GetCodeHash(addr common.Address) common.Hash {
	acc := s.sdb.GetAccount([20]byte(addr))
	if acc == nil {
		return common.Hash{}
	}
	return common.BytesToHash(acc.CodeHash[:])
}

// GetCode mengambil kode kontrak akun.
func (s *StateAdapter) GetCode(addr common.Address) []byte {
	return s.sdb.GetCode([20]byte(addr))
}

// SetCode mengatur kode kontrak akun.
func (s *StateAdapter) SetCode(addr common.Address, code []byte) {
	s.sdb.SetCode([20]byte(addr), code)
}

// GetCodeSize mengambil ukuran kode kontrak akun.
func (s *StateAdapter) GetCodeSize(addr common.Address) int {
	return len(s.sdb.GetCode([20]byte(addr)))
}

// AddRefund menambahkan gas ke refund counter.
func (s *StateAdapter) AddRefund(gas uint64) {}

// SubRefund mengurangi gas dari refund counter.
func (s *StateAdapter) SubRefund(gas uint64) {}

// GetRefund mengambil total gas yang akan di-refund.
func (s *StateAdapter) GetRefund() uint64 { return 0 }

// GetCommittedState mengambil state yang sudah di-commit dari storage akun.
func (s *StateAdapter) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	val := s.sdb.GetState([20]byte(addr), [32]byte(hash))
	return common.BytesToHash(val[:])
}

// GetState mengambil state dari storage akun.
func (s *StateAdapter) GetState(addr common.Address, hash common.Hash) common.Hash {
	val := s.sdb.GetState([20]byte(addr), [32]byte(hash))
	return common.BytesToHash(val[:])
}

// SetState mengatur state di storage akun.
func (s *StateAdapter) SetState(addr common.Address, key common.Hash, value common.Hash) {
	s.sdb.SetState([20]byte(addr), [32]byte(key), [32]byte(value))
}

// Suicide menandai akun untuk self-destruct.
func (s *StateAdapter) Suicide(addr common.Address) bool {
	logger.Debugf("StateAdapter: Suicide called for %s", addr.Hex())
	acc := s.sdb.GetAccount([20]byte(addr))
	if acc == nil || (acc.Nonce == 0 && (acc.Balance == nil || acc.Balance.Sign() == 0) && acc.CodeHash == ([32]byte{})) {
		return false
	}
	s.sdb.SetBalance([20]byte(addr), big.NewInt(0))
	s.sdb.SetNonce([20]byte(addr), 0)
	s.sdb.SetCode([20]byte(addr), nil)
	// Jika Anda memiliki metode ClearStorage di customState.StateDB, panggil di sini.
	// s.sdb.ClearStorage([20]byte(addr))
	return true
}

// HasSelfDestructed memeriksa apakah akun telah self-destruct.
func (s *StateAdapter) HasSelfDestructed(addr common.Address) bool {
	return false // Placeholder
}

// Exist memeriksa apakah akun ada.
func (s *StateAdapter) Exist(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	return acc.Nonce > 0 || (acc.Balance != nil && acc.Balance.Sign() > 0) || len(s.sdb.GetCode([20]byte(addr))) > 0
}

// Empty memeriksa apakah akun kosong.
func (s *StateAdapter) Empty(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	hasCode := acc.CodeHash != ([32]byte{}) || len(s.sdb.GetCode([20]byte(addr))) > 0
	return acc.Nonce == 0 && (acc.Balance == nil || acc.Balance.Sign() == 0) && !hasCode
}

// PrepareAccessList mempersiapkan access list untuk transaksi (EIP-2930).
func (s *StateAdapter) PrepareAccessList(sender common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethTypes.AccessList) {
}

// AddressInAccessList memeriksa apakah alamat ada dalam access list.
func (s *StateAdapter) AddressInAccessList(addr common.Address) bool {
	return true
}

// SlotInAccessList memeriksa apakah slot storage ada dalam access list.
func (s *StateAdapter) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return true, true
}

// AddAddressToAccessList menambahkan alamat ke access list.
func (s *StateAdapter) AddAddressToAccessList(addr common.Address) {}

// AddSlotToAccessList menambahkan slot storage ke access list.
func (s *StateAdapter) AddSlotToAccessList(addr common.Address, slot common.Hash) {}

// RevertToSnapshot mengembalikan state ke snapshot sebelumnya.
func (s *StateAdapter) RevertToSnapshot(id int) {
	s.sdb.RevertToSnapshot(id)
}

// Snapshot membuat snapshot dari state saat ini.
func (s *StateAdapter) Snapshot() int {
	return s.sdb.Snapshot()
}

// AddLog menambahkan log ke StateDB.
func (s *StateAdapter) AddLog(gethLog *ethTypes.Log) {
	customLog := &customState.Log{
		Address:     [20]byte(gethLog.Address),
		Topics:      make([][32]byte, len(gethLog.Topics)),
		Data:        common.CopyBytes(gethLog.Data),
		BlockNumber: gethLog.BlockNumber,
		TxHash:      [32]byte(gethLog.TxHash),
		TxIndex:     uint64(gethLog.TxIndex),
		BlockHash:   [32]byte(gethLog.BlockHash),
		Index:       uint64(gethLog.Index),
	}
	for i, topic := range gethLog.Topics {
		customLog.Topics[i] = [32]byte(topic)
	}
	s.sdb.AddLog(customLog)
}

// AddPreimage menambahkan preimage hash ke StateDB.
func (s *StateAdapter) AddPreimage(hash common.Hash, preimage []byte) {}

// ForEachStorage melakukan iterasi pada semua slot storage untuk sebuah akun.
func (s *StateAdapter) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	logger.Warningf("StateAdapter: ForEachStorage called for %s (Not fully implemented, returning error)", addr.Hex())
	return errors.New("StateAdapter.ForEachStorage not implemented")
}

// AccessEvents mengembalikan daftar perubahan saldo yang terlacak.
// Mengembalikan *state.AccessEvents (pointer ke slice).
func (s *StateAdapter) AccessEvents() *state.AccessEvents {
	logger.Debugf("StateAdapter: AccessEvents called (returning nil as placeholder for *state.AccessEvents)")
	return nil
}

// CanTransfer memeriksa apakah transfer mungkin dilakukan.
func (s *StateAdapter) CanTransfer(db vm.StateDB, addr common.Address, amount *uint256.Int) bool {
	balanceUint256 := db.GetBalance(addr)
	return balanceUint256.Cmp(amount) >= 0
}

// Transfer melakukan transfer saldo.
func (s *StateAdapter) Transfer(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	db.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
	db.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
}

// Pastikan StateAdapter mengimplementasikan vm.StateDB
var _ vm.StateDB = (*StateAdapter)(nil)
