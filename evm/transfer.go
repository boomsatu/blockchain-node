package evm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state" // Ini adalah state.StateDB dari go-ethereum
)

// CanTransfer checks whether there are enough funds in the address' account to make a transfer.
// Perhatian: db di sini adalah go-ethereum/core/state.StateDB, bukan state.StateDB Anda.
// Anda perlu adapter jika ingin ini bekerja dengan state.StateDB Anda.
func CanTransfer(db state.StateDB, addr common.Address, amount *big.Int) bool {
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
// Perhatian: db di sini adalah go-ethereum/core/state.StateDB.
func Transfer(db state.StateDB, sender, recipient common.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
