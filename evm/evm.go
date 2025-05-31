package evm // Diperbaiki: package evm

import (
	"blockchain-node/interfaces"
	"blockchain-node/logger"

	// customState "blockchain-node/state" // Tidak perlu di sini jika ExecutionResult.Logs diisi dari StateAdapter
	"errors"
	"math/big" // Masih dibutuhkan untuk konversi ke/dari uint256

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm" // Diperlukan untuk chainRules
	"github.com/holiman/uint256"              // Untuk tipe uint256.Int
)

// Blockchain mendefinisikan antarmuka yang dibutuhkan EVM untuk mengakses data spesifik blockchain.
type Blockchain interface {
	GetConfig() interfaces.ChainConfigItf
	GetBlockByNumber(number uint64) interfaces.BlockHeaderItf
}

// EVM adalah wrapper di sekitar EVM go-ethereum.
type EVM struct {
	blockchain Blockchain
	vmConfig   vm.Config
}

// NewEVM membuat instance EVM baru.
func NewEVM(blockchain Blockchain) *EVM {
	config := vm.Config{
		// Debug: true,
		// Tracer: logger.NewTracer("EVM_DETAIL"),
		// NoBaseFee: true,
	}
	return &EVM{
		blockchain: blockchain,
		vmConfig:   config,
	}
}

// ExecuteTransaction menjalankan transaksi menggunakan EVM.
func (e *EVM) ExecuteTransaction(ctx *interfaces.ExecutionContext) (*interfaces.ExecutionResult, error) {
	sdb := ctx.StateDB
	txItf := ctx.Transaction
	headerItf := ctx.BlockHeader
	gasUsedPool := ctx.GasUsedPool

	stateAdapter := NewStateAdapter(sdb)

	var coinbase common.Address
	minerAddrBytes := headerItf.GetMiner()
	if minerAddrBytes != ([20]byte{}) {
		coinbase = common.BytesToAddress(minerAddrBytes[:])
	}

	blockContext := vm.BlockContext{
		CanTransfer: stateAdapter.CanTransfer,
		Transfer:    stateAdapter.Transfer,
		GetHash:     e.GetHashFn(headerItf),
		Coinbase:    coinbase,
		BlockNumber: new(big.Int).SetUint64(headerItf.GetNumber()),
		Time:        uint64(headerItf.GetTimestamp()),
		Difficulty:  new(big.Int).Set(headerItf.GetDifficulty()),
		GasLimit:    headerItf.GetGasLimit(),
		BaseFee:     nil,
	}

	chainRules := e.blockchain.GetConfig().ToEthChainConfig()
	evmInstance := vm.NewEVM(blockContext, stateAdapter, chainRules, e.vmConfig)

	snapshotID := stateAdapter.Snapshot()

	var returnData []byte
	var vmError error
	var gasLeft uint64
	var createdContractAddress common.Address

	txData := txItf.GetData()
	txGasLimit := txItf.GetGasLimit()

	txValueUint256, overflow := uint256.FromBig(txItf.GetValue())
	if overflow {
		err := errors.New("transaction value overflowed uint256")
		logger.Errorf("EVM.ExecuteTransaction: %v for tx %x", err, txItf.GetHash())
		if gasUsedPool != nil {
			gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit))
		}
		stateAdapter.RevertToSnapshot(snapshotID)
		return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
	}

	txOrigin := common.Address(txItf.GetFrom())
	callerRef := vm.AccountRef(txOrigin) // vm.AccountRef is defined in go-ethereum/core/vm

	if txItf.IsContractCreation() {
		returnData, createdContractAddress, gasLeft, vmError = evmInstance.Create(
			callerRef,
			txData,
			txGasLimit,
			txValueUint256,
		)
	} else {
		txToPtr := txItf.GetTo()
		if txToPtr == nil {
			err := errors.New("transaction 'to' address is nil for non-contract creation")
			if gasUsedPool != nil {
				gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit))
			}
			stateAdapter.RevertToSnapshot(snapshotID)
			return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
		}
		toAddr := common.BytesToAddress((*txToPtr)[:])
		returnData, gasLeft, vmError = evmInstance.Call(
			callerRef,
			toAddr,
			txData,
			txGasLimit,
			txValueUint256,
		)
	}

	actualGasUsed := txGasLimit - gasLeft
	finalLogs := []*interfaces.Log{}

	if vmError != nil {
		logger.Warningf("EVM execution error for tx %x: %v. Gas used: %d. Reverting to snapshot %d.", txItf.GetHash(), vmError, actualGasUsed, snapshotID)
		stateAdapter.RevertToSnapshot(snapshotID)
	} else {
		sdbLogs := sdb.GetLogs(txItf.GetHash())
		for _, sdbLog := range sdbLogs {
			topics := make([]common.Hash, len(sdbLog.Topics))
			for i, t := range sdbLog.Topics {
				topics[i] = common.BytesToHash(t[:])
			}
			finalLogs = append(finalLogs, &interfaces.Log{
				Address:     common.BytesToAddress(sdbLog.Address[:]),
				Topics:      topics,
				Data:        sdbLog.Data,
				BlockNumber: sdbLog.BlockNumber,
				TxHash:      common.BytesToHash(sdbLog.TxHash[:]),
				TxIndex:     uint(sdbLog.TxIndex),
				BlockHash:   common.BytesToHash(sdbLog.BlockHash[:]),
				Index:       uint(sdbLog.Index),
			})
		}
		logger.Debugf("EVM execution successful for tx %x. Gas used: %d. Logs retrieved: %d", txItf.GetHash(), actualGasUsed, len(finalLogs))
	}

	executionResult := &interfaces.ExecutionResult{
		GasUsed:    actualGasUsed,
		Status:     1,
		ReturnData: returnData,
		Logs:       finalLogs,
		Err:        vmError,
	}

	if vmError != nil {
		executionResult.Status = 0
	}

	if txItf.IsContractCreation() && executionResult.Status == 1 {
		if createdContractAddress != (common.Address{}) {
			var contractAddrBytes [20]byte
			copy(contractAddrBytes[:], createdContractAddress.Bytes())
			executionResult.ContractAddress = &contractAddrBytes
		} else {
			logger.Debugf("Contract creation for tx %x resulted in empty contract address.", txItf.GetHash())
		}
	}

	if gasUsedPool != nil {
		gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(actualGasUsed))
	}

	return executionResult, vmError
}

func (e *EVM) GetHashFn(currentProcessingHeader interfaces.BlockHeaderItf) vm.GetHashFunc {
	return func(blockNumToQuery uint64) common.Hash {
		if blockNumToQuery >= currentProcessingHeader.GetNumber() {
			return common.Hash{}
		}
		headerItf := e.blockchain.GetBlockByNumber(blockNumToQuery)
		if headerItf != nil {
			hashBytes := headerItf.GetHash()
			return common.BytesToHash(hashBytes[:])
		}
		return common.Hash{}
	}
}

var _ interfaces.VirtualMachine = (*EVM)(nil)
