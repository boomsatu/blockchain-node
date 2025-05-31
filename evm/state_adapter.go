package evm

import (
	"blockchain-node/logger" // Pastikan logger diimpor jika digunakan
	customState "blockchain-node/state"
	"math/big"

	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"        // Diperlukan untuk BalanceChangeReason
	ethTypes "github.com/ethereum/go-ethereum/core/types" // types.Log dari Geth
	"github.com/ethereum/go-ethereum/core/vm"
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

func (s *StateAdapter) CreateAccount(addr common.Address) {
	_ = s.sdb.GetAccount([20]byte(addr))
}

// SubBalance sekarang menerima BalanceChangeReason
func (s *StateAdapter) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	amountBigInt := amount.ToBig()
	s.sdb.SubBalance([20]byte(addr), amountBigInt)
	logger.Debugf("StateAdapter: SubBalance for %s, amount %s, reason: %v", addr.Hex(), amount.String(), reason)
}

// AddBalance sekarang menerima BalanceChangeReason
func (s *StateAdapter) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	amountBigInt := amount.ToBig()
	s.sdb.AddBalance([20]byte(addr), amountBigInt)
	logger.Debugf("StateAdapter: AddBalance for %s, amount %s, reason: %v", addr.Hex(), amount.String(), reason)
}

func (s *StateAdapter) GetBalance(addr common.Address) *uint256.Int {
	balanceBigInt := s.sdb.GetBalance([20]byte(addr))
	balanceUint256, _ := uint256.FromBig(balanceBigInt)
	return balanceUint256
}

func (s *StateAdapter) GetNonce(addr common.Address) uint64 {
	return s.sdb.GetNonce([20]byte(addr))
}

func (s *StateAdapter) SetNonce(addr common.Address, nonce uint64) {
	s.sdb.SetNonce([20]byte(addr), nonce)
}

func (s *StateAdapter) GetCodeHash(addr common.Address) common.Hash {
	acc := s.sdb.GetAccount([20]byte(addr))
	if acc == nil {
		return common.Hash{}
	}
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

func (s *StateAdapter) AddRefund(gas uint64) {}
func (s *StateAdapter) SubRefund(gas uint64) {}
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

func (s *StateAdapter) Suicide(addr common.Address) bool {
	logger.Debugf("StateAdapter: Suicide called for %s", addr.Hex())
	acc := s.sdb.GetAccount([20]byte(addr))
	if acc == nil || (acc.Nonce == 0 && acc.Balance.Sign() == 0 && acc.CodeHash == ([32]byte{})) {
		return false
	}
	s.sdb.SetBalance([20]byte(addr), big.NewInt(0))
	s.sdb.SetNonce([20]byte(addr), 0)
	s.sdb.SetCode([20]byte(addr), nil)
	// s.sdb.ClearStorage([20]byte(addr)) // Jika ada
	return true
}

func (s *StateAdapter) HasSelfDestructed(addr common.Address) bool {
	return false
}

func (s *StateAdapter) Exist(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	return acc.Nonce > 0 || (acc.Balance != nil && acc.Balance.Sign() > 0) || len(s.sdb.GetCode([20]byte(addr))) > 0
}

func (s *StateAdapter) Empty(addr common.Address) bool {
	acc := s.sdb.GetAccount([20]byte(addr))
	hasCode := acc.CodeHash != ([32]byte{}) || len(s.sdb.GetCode([20]byte(addr))) > 0
	return acc.Nonce == 0 && (acc.Balance == nil || acc.Balance.Sign() == 0) && !hasCode
}

func (s *StateAdapter) PrepareAccessList(sender common.Address, dest *common.Address, precompiles []common.Address, txAccesses ethTypes.AccessList) {
}
func (s *StateAdapter) AddressInAccessList(addr common.Address) bool {
	return true
}
func (s *StateAdapter) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	return true, true
}
func (s *StateAdapter) AddAddressToAccessList(addr common.Address)                {}
func (s *StateAdapter) AddSlotToAccessList(addr common.Address, slot common.Hash) {}

func (s *StateAdapter) RevertToSnapshot(id int) {
	s.sdb.RevertToSnapshot(id)
}
func (s *StateAdapter) Snapshot() int {
	return s.sdb.Snapshot()
}

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

func (s *StateAdapter) AddPreimage(hash common.Hash, preimage []byte) {}

func (s *StateAdapter) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	return errors.New("StateAdapter.ForEachStorage not implemented")
}

func (s *StateAdapter) AccessEvents() []tracing.BalanceChangeHook {
	logger.Debugf("StateAdapter: AccessEvents called (returning nil as placeholder)")
	return nil
}

func (s *StateAdapter) CanTransfer(db vm.StateDB, addr common.Address, amount *uint256.Int) bool {
	balanceUint256 := s.GetBalance(addr)
	return balanceUint256.Cmp(amount) >= 0
}

func (s *StateAdapter) Transfer(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	s.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
	s.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
}

var _ vm.StateDB = (*StateAdapter)(nil)
