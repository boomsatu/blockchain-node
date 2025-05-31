package execution

import (
	"blockchain-node/interfaces"
	// "blockchain-node/state" // Tidak lagi dibutuhkan di sini, stateDB datang dari ExecutionContext
	"fmt"
	"math/big"
)

type VirtualMachine struct {
	// Tidak ada field yang dibutuhkan di sini lagi
}

func NewVirtualMachine() *VirtualMachine {
	return &VirtualMachine{}
}

func (vm *VirtualMachine) ExecuteTransaction(ctx *interfaces.ExecutionContext) (*interfaces.ExecutionResult, error) {
	sdb := ctx.StateDB
	txItf := ctx.Transaction
	gasUsedPool := ctx.GasUsedPool

	var calculatedGasUsed uint64 = 21000 // Biaya dasar
	if txItf.IsContractCreation() {
		calculatedGasUsed = 53000 // Biaya dasar pembuatan kontrak
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
		}, err
	}

	status := uint64(1)
	var execErr error
	var logs []interfaces.Log

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
				Logs:    logs,
			}, execErr
		}

		// Lakukan transfer
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

	return &interfaces.ExecutionResult{
		GasUsed: calculatedGasUsed,
		Status:  status,
		Logs:    logs,
		Err:     execErr,
	}, execErr
}

var (
	ErrInsufficientBalance = fmt.Errorf("insufficient balance for transfer")
)
