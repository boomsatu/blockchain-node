package evm

import (
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm" // Pastikan import ini benar
	"github.com/holiman/uint256"
)

// Blockchain defines the interface the EVM needs to access blockchain-specific data.
// This should match what your core.Blockchain provides.
type Blockchain interface {
	GetConfig() interfaces.ChainConfigItf                           // Returns the chain configuration (for chainRules)
	GetBlockByNumberForEVM(number uint64) interfaces.BlockHeaderItf // Function for GetHashFn (name adjusted)
}

// EVM is a wrapper around the go-ethereum EVM.
type EVM struct {
	blockchain Blockchain // Uses the Blockchain interface defined above
	vmConfig   vm.Config  // Configuration for the EVM instance
}

// NewEVM creates a new EVM instance.
func NewEVM(blockchain Blockchain) *EVM {
	config := vm.Config{
		// Debug: true, // Enable for deep EVM debugging if needed
		// Tracer: logger.NewTracer("EVM_DETAIL"), // You'd need a tracer implementation
		// NoBaseFee: true, // Set true if your chain doesn't use EIP-1559 base fee
	}
	return &EVM{
		blockchain: blockchain,
		vmConfig:   config,
	}
}

// ExecuteTransaction runs a transaction using the EVM.
func (e *EVM) ExecuteTransaction(ctx *interfaces.ExecutionContext) (*interfaces.ExecutionResult, error) {
	sdb := ctx.StateDB // This is your *customState.StateDB
	txItf := ctx.Transaction
	headerItf := ctx.BlockHeader
	gasUsedPool := ctx.GasUsedPool // This is a *big.Int for accumulating gas

	// Create a StateAdapter from your customState.StateDB
	stateAdapter := NewStateAdapter(sdb)

	var coinbase common.Address
	minerAddrBytes := headerItf.GetMiner()
	if minerAddrBytes != ([20]byte{}) { // Ensure miner address is not zero
		coinbase = common.BytesToAddress(minerAddrBytes[:])
	}

	// Prepare the BlockContext for the EVM
	blockContext := vm.BlockContext{
		CanTransfer: stateAdapter.CanTransfer, // Using method from StateAdapter
		Transfer:    stateAdapter.Transfer,    // Using method from StateAdapter
		GetHash:     e.GetHashFn(headerItf),   // Function to get previous block hashes
		Coinbase:    coinbase,
		BlockNumber: new(big.Int).SetUint64(headerItf.GetNumber()),
		Time:        uint64(headerItf.GetTimestamp()), // Block timestamp
		Difficulty:  new(big.Int).Set(headerItf.GetDifficulty()),
		GasLimit:    headerItf.GetGasLimit(),
		BaseFee:     nil, // Set appropriately if your chain uses EIP-1559
		// Random:      nil, // For PREVRANDAO (post-Merge), can be ignored for simple PoW chains
	}

	// Get chain rules from your blockchain configuration
	chainRules := e.blockchain.GetConfig().ToEthChainConfig()
	if chainRules == nil {
		return nil, errors.New("EVM.ExecuteTransaction: chainRules are nil, blockchain config might be missing")
	}

	// Create a new EVM instance
	// Ensure stateAdapter now correctly implements vm.StateDB (including CreateContract and AccessEvents returning *state.AccessEvents)
	evmInstance := vm.NewEVM(blockContext, stateAdapter, chainRules, e.vmConfig)

	// Take a snapshot of the stateDB before execution
	snapshotID := stateAdapter.Snapshot()

	var returnData []byte
	var vmError error
	var gasLeft uint64
	var createdContractAddress common.Address

	txData := txItf.GetData()
	txGasLimit := txItf.GetGasLimit()

	// Convert transaction value to *uint256.Int
	txValueUint256, overflow := uint256.FromBig(txItf.GetValue())
	if overflow {
		err := errors.New("transaction value overflowed uint256")
		logger.Errorf("EVM.ExecuteTransaction: %v for tx %x", err, txItf.GetHash())
		if gasUsedPool != nil {
			gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit)) // All gas is considered used
		}
		stateAdapter.RevertToSnapshot(snapshotID) // Revert state
		return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
	}

	txOrigin := common.Address(txItf.GetFrom())
	// Use vm.NewContractRef to create a reference to the caller's account/contract
	caller := vm.NewContractRef(txOrigin)

	// Execute the transaction
	if txItf.IsContractCreation() {
		returnData, createdContractAddress, gasLeft, vmError = evmInstance.Create(
			caller,         // Caller (EOA or contract)
			txData,         // Contract init code
			txGasLimit,     // Gas limit for the transaction
			txValueUint256, // Value sent with contract creation
		)
	} else {
		txToPtr := txItf.GetTo()
		if txToPtr == nil { // Should not happen if IsContractCreation() is false
			err := errors.New("transaction 'to' address is nil for non-contract creation")
			if gasUsedPool != nil {
				gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit))
			}
			stateAdapter.RevertToSnapshot(snapshotID)
			return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
		}
		toAddr := common.BytesToAddress((*txToPtr)[:])
		returnData, gasLeft, vmError = evmInstance.Call(
			caller,         // Caller
			toAddr,         // Recipient address (contract or EOA)
			txData,         // Input data for the call
			txGasLimit,     // Gas limit
			txValueUint256, // Value sent
		)
	}

	actualGasUsed := txGasLimit - gasLeft
	finalLogs := []*interfaces.Log{} // Initialize empty slice for logs

	if vmError != nil {
		logger.Warningf("EVM execution error for tx %x: %v. Gas used: %d. Reverting to snapshot %d.", txItf.GetHash(), vmError, actualGasUsed, snapshotID)
		stateAdapter.RevertToSnapshot(snapshotID)
		// Logs added before the error by the EVM would be reverted by the snapshot.
	} else {
		// If no VM error, retrieve logs from the stateDB
		// StateAdapter.AddLog would have been called by the EVM during execution.
		// sdb.GetLogs(txItf.GetHash()) should retrieve logs relevant to this transaction.
		sdbLogs := sdb.GetLogs(txItf.GetHash()) // Assumes GetLogs retrieves logs for a specific tx from the current block's logs
		for _, sdbLog := range sdbLogs {
			// Convert customState.Log to interfaces.Log
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
				TxIndex:     uint(sdbLog.TxIndex), // Adjust type if necessary
				BlockHash:   common.BytesToHash(sdbLog.BlockHash[:]),
				Index:       uint(sdbLog.Index), // Adjust type if necessary
			})
		}
		logger.Debugf("EVM execution successful for tx %x. Gas used: %d. Logs retrieved from StateDB: %d", txItf.GetHash(), actualGasUsed, len(finalLogs))
	}

	executionResult := &interfaces.ExecutionResult{
		GasUsed:    actualGasUsed,
		Status:     1, // Assume success unless vmError indicates otherwise
		ReturnData: returnData,
		Logs:       finalLogs,
		Err:        vmError, // Store the error from EVM
	}

	if vmError != nil {
		executionResult.Status = 0 // Set status to failed if there was an EVM error
	}

	if txItf.IsContractCreation() && executionResult.Status == 1 {
		if createdContractAddress != (common.Address{}) {
			var contractAddrBytes [20]byte
			copy(contractAddrBytes[:], createdContractAddress.Bytes())
			executionResult.ContractAddress = &contractAddrBytes
		} else {
			// This can happen if the init code reverts or has an internal error,
			// even if vmError is nil.
			logger.Debugf("Contract creation for tx %x resulted in empty contract address, vmError: %v", txItf.GetHash(), vmError)
		}
	}

	// Add gas used to the block's gas pool
	if gasUsedPool != nil {
		gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(actualGasUsed))
	}

	return executionResult, vmError // Also return vmError so the caller is aware
}

// GetHashFn is a closure function used by the EVM to get previous block hashes.
func (e *EVM) GetHashFn(currentProcessingHeader interfaces.BlockHeaderItf) vm.GetHashFunc {
	return func(blockNumToQuery uint64) common.Hash {
		// Do not query for the current block or future blocks relative to the one being processed.
		if blockNumToQuery >= currentProcessingHeader.GetNumber() {
			return common.Hash{}
		}
		// Use the method from the Blockchain interface passed to EVM
		headerItf := e.blockchain.GetBlockByNumberForEVM(blockNumToQuery)
		if headerItf != nil {
			hashBytes := headerItf.GetHash() // Assumes GetHash() returns [32]byte
			return common.BytesToHash(hashBytes[:])
		}
		return common.Hash{} // Return empty hash if block not found
	}
}

// Ensure EVM implements interfaces.VirtualMachine
var _ interfaces.VirtualMachine = (*EVM)(nil)
