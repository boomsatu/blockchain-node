// File: evm/state_adapter.go
package evm

import (
	"blockchain-node/logger" // Pastikan logger diimpor jika digunakan
	customState "blockchain-node/state"
	"math/big"

	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"   // Digunakan untuk state.AccessEvents
	"github.com/ethereum/go-ethereum/core/tracing" // Dikembalikan karena vm.StateDB tampaknya masih memerlukan ini
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm" // Impor utama untuk vm.StateDB
	"github.com/holiman/uint256"              // Untuk tipe uint256.Int
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
// Metode ini adalah bagian dari vm.StateDB.
func (s *StateAdapter) CreateAccount(addr common.Address) {
	_ = s.sdb.GetAccount([20]byte(addr))
	logger.Debugf("StateAdapter: CreateAccount called for %s", addr.Hex())
}

// SubBalance mengurangi saldo akun.
// Tanda tangan dikembalikan untuk menyertakan 'reason' sesuai dengan pesan error.
func (s *StateAdapter) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	amountBigInt := amount.ToBig() // Konversi *uint256.Int ke *big.Int
	s.sdb.SubBalance([20]byte(addr), amountBigInt)
	logger.Debugf("StateAdapter: SubBalance for %s, amount %s, reason: %v", addr.Hex(), amount.String(), reason)
}

// AddBalance menambah saldo akun.
// Tanda tangan dikembalikan untuk menyertakan 'reason' sesuai dengan pesan error.
func (s *StateAdapter) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	amountBigInt := amount.ToBig() // Konversi *uint256.Int ke *big.Int
	s.sdb.AddBalance([20]byte(addr), amountBigInt)
	logger.Debugf("StateAdapter: AddBalance for %s, amount %s, reason: %v", addr.Hex(), amount.String(), reason)
}

// GetBalance mengambil saldo akun. Mengembalikan *uint256.Int.
func (s *StateAdapter) GetBalance(addr common.Address) *uint256.Int {
	balanceBigInt := s.sdb.GetBalance([20]byte(addr))
	if balanceBigInt == nil {
		return uint256.NewInt(0)
	}
	balanceUint256, overflow := uint256.FromBig(balanceBigInt)
	if overflow {
		logger.Errorf("StateAdapter: GetBalance - balance overflowed uint256 for address %s. Original big.Int: %s", addr.Hex(), balanceBigInt.String())
		if balanceUint256 == nil {
			return uint256.NewInt(0)
		}
		return balanceUint256
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
	code := s.sdb.GetCode([20]byte(addr))
	return len(code)
}

// AddRefund menambahkan gas ke refund counter.
func (s *StateAdapter) AddRefund(gas uint64) {
	// Implementasi placeholder
}

// SubRefund mengurangi gas dari refund counter.
func (s *StateAdapter) SubRefund(gas uint64) {
	// Implementasi placeholder
}

// GetRefund mengambil total gas yang akan di-refund.
func (s *StateAdapter) GetRefund() uint64 {
	return 0 // Implementasi placeholder
}

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
	if !s.Exist(addr) {
		return false
	}
	s.sdb.SetBalance([20]byte(addr), big.NewInt(0))
	s.sdb.SetNonce([20]byte(addr), 0)
	s.sdb.SetCode([20]byte(addr), nil)
	logger.Warningf("StateAdapter: Suicide for %s - Storage clearing mechanism needs to be ensured in customState.StateDB", addr.Hex())
	return true
}

// HasSelfDestructed memeriksa apakah akun telah self-destruct.
func (s *StateAdapter) HasSelfDestructed(addr common.Address) bool {
	return false // Placeholder
}

// Exist memeriksa apakah akun ada.
func (s *StateAdapter) Exist(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	hasCode := len(s.sdb.GetCode([20]byte(addr))) > 0
	return acc.Nonce > 0 || (acc.Balance != nil && acc.Balance.Sign() > 0) || hasCode
}

// Empty memeriksa apakah akun kosong.
func (s *StateAdapter) Empty(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	hasCode := len(s.sdb.GetCode([20]byte(addr))) > 0
	return acc.Nonce == 0 && (acc.Balance == nil || acc.Balance.Sign() == 0) && !hasCode
}

// PrepareAccessList mempersiapkan access list untuk transaksi (EIP-2930).
func (s *StateAdapter) PrepareAccessList(sender common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethTypes.AccessList) {
	// Implementasi placeholder
}

// AddressInAccessList memeriksa apakah alamat ada dalam access list.
func (s *StateAdapter) AddressInAccessList(addr common.Address) bool {
	return true // Implementasi placeholder
}

// SlotInAccessList memeriksa apakah slot storage ada dalam access list.
func (s *StateAdapter) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return true, true // Implementasi placeholder
}

// AddAddressToAccessList menambahkan alamat ke access list.
func (s *StateAdapter) AddAddressToAccessList(addr common.Address) {
	// Implementasi placeholder
}

// AddSlotToAccessList menambahkan slot storage ke access list.
func (s *StateAdapter) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	// Implementasi placeholder
}

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
	logger.Debugf("StateAdapter: AddLog called for address %s, topics count: %d", gethLog.Address.Hex(), len(gethLog.Topics))
}

// AddPreimage menambahkan preimage hash ke StateDB.
func (s *StateAdapter) AddPreimage(hash common.Hash, preimage []byte) {
	// Implementasi placeholder
}

// ForEachStorage melakukan iterasi pada semua slot storage untuk sebuah akun.
func (s *StateAdapter) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	logger.Warningf("StateAdapter: ForEachStorage called for %s (Not fully implemented, returning error as placeholder)", addr.Hex())
	return errors.New("StateAdapter.ForEachStorage not implemented")
}

// CanTransfer memeriksa apakah transfer mungkin dilakukan.
func (s *StateAdapter) CanTransfer(db vm.StateDB, addr common.Address, amount *uint256.Int) bool {
	balanceUint256 := db.GetBalance(addr)
	return balanceUint256.Cmp(amount) >= 0
}

// Transfer melakukan transfer saldo.
// Pemanggilan db.SubBalance dan db.AddBalance sekarang menyertakan reason.
func (s *StateAdapter) Transfer(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	// Ketika EVM memanggil fungsi Transfer ini (melalui BlockContext),
	// 'db' adalah instance StateDB (yaitu StateAdapter ini sendiri).
	// Jadi, kita memanggil metode SubBalance dan AddBalance dari StateAdapter,
	// yang sekarang mengharapkan argumen 'reason'.
	db.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
	db.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
}

// AccessListStatus mengembalikan status slot dalam access list.
// Jika state.AccessStatus dan state.ColdAccess masih undefined, ini akan error.
// Ini menandakan masalah dependensi go-ethereum atau versi yang tidak cocok.
func (s *StateAdapter) AccessListStatus(addr common.Address, slot common.Hash) state.AccessStatus {
	logger.Debugf("StateAdapter: AccessListStatus called for addr %s, slot %s (Returning ColdAccess as placeholder)", addr.Hex(), slot.Hex()) // Mengasumsikan state.ColdAccess terdefinisi.
	return state.ColdAccess                                                                                                                   // Mengasumsikan state.ColdAccess terdefinisi.
}

// AccessEvents mengembalikan daftar perubahan saldo yang terlacak.
// Ini diperlukan oleh antarmuka vm.StateDB di versi go-ethereum yang lebih baru.
func (s *StateAdapter) AccessEvents() *state.AccessEvents {
	logger.Debugf("StateAdapter: AccessEvents called (returning nil as placeholder)")
	// Jika StateDB kustom Anda tidak melacak AccessEvents secara detail,
	// mengembalikan nil adalah pilihan yang masuk akal.
	// EVM mungkin menggunakan ini untuk tujuan tertentu seperti tracing atau analisis gas.
	return nil
}

// Pastikan StateAdapter mengimplementasikan vm.StateDB
var _ vm.StateDB = (*StateAdapter)(nil)
