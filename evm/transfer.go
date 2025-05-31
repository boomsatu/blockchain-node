package evm

import (
	// "math/big" // Tidak lagi dibutuhkan jika menggunakan uint256

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing" // Diperlukan untuk BalanceChangeReason
	"github.com/ethereum/go-ethereum/core/vm"      // Menggunakan vm.StateDB untuk signature
	"github.com/holiman/uint256"                   // Untuk tipe uint256.Int
)

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// Fungsi ini sekarang menggunakan vm.StateDB dan *uint256.Int, konsisten dengan StateAdapter.
func CanTransfer(db vm.StateDB, addr common.Address, amount *uint256.Int) bool {
	// db.GetBalance(addr) akan mengembalikan *uint256.Int jika db adalah StateAdapter kita
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db.
// Fungsi ini sekarang menggunakan vm.StateDB dan *uint256.Int.
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	// db.SubBalance dan db.AddBalance akan menerima *uint256.Int jika db adalah StateAdapter kita
	db.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
	db.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
}
