package execution

import (
	"blockchain-node/interfaces"
	// "blockchain-node/state" // Tidak lagi dibutuhkan di sini, stateDB datang dari ExecutionContext
	"fmt"
	"math/big"
	// "github.com/ethereum/go-ethereum/common" // Hanya dibutuhkan jika VM ini membuat log sendiri
)

// VirtualMachine adalah implementasi sederhana dari Virtual Machine.
// Jika Anda menggunakan EVM dari go-ethereum, implementasi utama akan ada di paket evm.
// File ini mungkin menjadi placeholder atau berisi logika VM yang sangat disederhanakan
// jika tidak menggunakan EVM go-ethereum secara langsung di sini.
type VirtualMachine struct {
	// Tidak ada field yang dibutuhkan di sini jika ini adalah VM sederhana.
}

// NewVirtualMachine membuat instance VirtualMachine baru.
func NewVirtualMachine() *VirtualMachine {
	return &VirtualMachine{}
}

// ExecuteTransaction adalah contoh implementasi sederhana.
// Jika Anda menggunakan EVM go-ethereum, logika utama ada di evm.EVM.ExecuteTransaction.
// Fungsi ini mungkin tidak lagi menjadi titik masuk utama jika evm.EVM digunakan.
func (vm *VirtualMachine) ExecuteTransaction(ctx *interfaces.ExecutionContext) (*interfaces.ExecutionResult, error) {
	sdb := ctx.StateDB
	txItf := ctx.Transaction
	gasUsedPool := ctx.GasUsedPool

	var calculatedGasUsed uint64 = 21000 // Biaya dasar
	if txItf.IsContractCreation() {
		calculatedGasUsed = 53000
		for _, b := range txItf.GetData() {
			if b == 0 {
				calculatedGasUsed += 4
			} else {
				calculatedGasUsed += 16
			}
		}
	} else if len(txItf.GetData()) > 0 {
		for _, b := range txItf.GetData() {
			if b == 0 {
				calculatedGasUsed += 4
			} else {
				calculatedGasUsed += 16
			}
		}
	}

	if txItf.GetGasLimit() < calculatedGasUsed {
		if gasUsedPool != nil {
			gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txItf.GetGasLimit()))
		}
		err := fmt.Errorf("out of gas: intrinsic gas required %d, available %d", calculatedGasUsed, txItf.GetGasLimit())
		return &interfaces.ExecutionResult{
			GasUsed: txItf.GetGasLimit(),
			Status:  0,
			Err:     err,
			Logs:    []*interfaces.Log{}, // Diperbaiki: Inisialisasi sebagai slice of pointers
		}, err
	}

	status := uint64(1)
	var execErr error
	var logs []*interfaces.Log // Diperbaiki: Deklarasi sebagai slice of pointers

	txValue := txItf.GetValue()
	txFrom := txItf.GetFrom()
	txToPtr := txItf.GetTo()

	if txValue.Cmp(big.NewInt(0)) > 0 {
		senderBalance := sdb.GetBalance(txFrom)
		if senderBalance.Cmp(txValue) < 0 {
			status = 0
			execErr = ErrInsufficientBalance
			if gasUsedPool != nil {
				gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(calculatedGasUsed))
			}
			return &interfaces.ExecutionResult{
				GasUsed: calculatedGasUsed,
				Status:  status,
				Err:     execErr,
				Logs:    logs, // logs sudah bertipe []*interfaces.Log
			}, execErr
		}

		newSenderBalance := new(big.Int).Sub(senderBalance, txValue)
		sdb.SetBalance(txFrom, newSenderBalance)

		if txToPtr != nil {
			receiverBalance := sdb.GetBalance(*txToPtr)
			newReceiverBalance := new(big.Int).Add(receiverBalance, txValue)
			sdb.SetBalance(*txToPtr, newReceiverBalance)
		}
	}

	if gasUsedPool != nil {
		gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(calculatedGasUsed))
	}

	// Contoh penambahan log sederhana (jika VM ini menghasilkan log sendiri)
	// if status == 1 && txToPtr != nil {
	// 	logs = append(logs, &interfaces.Log{ // Tambahkan pointer ke Log
	// 		Address: common.BytesToAddress((*txToPtr)[:]),
	// 		Topics:  []common.Hash{},
	// 		Data:    []byte("simple transfer success from custom VM"),
	// 		// Isi field lain jika perlu (BlockNumber, TxHash, dll. dari ctx)
	// 		BlockNumber: ctx.BlockHeader.GetNumber(),
	// 		TxHash:      common.BytesToHash(ctx.Transaction.GetHash()[:]),
	// 		// TxIndex dan Index log perlu diatur dengan benar
	// 	})
	// }

	return &interfaces.ExecutionResult{
		GasUsed: calculatedGasUsed,
		Status:  status,
		Logs:    logs, // logs sudah bertipe []*interfaces.Log
		Err:     execErr,
	}, execErr
}

var (
	ErrInsufficientBalance = fmt.Errorf("insufficient balance for transfer")
)

var _ interfaces.VirtualMachine = (*VirtualMachine)(nil)
