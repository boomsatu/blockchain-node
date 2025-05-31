package evm

import (
	"blockchain-node/interfaces"
	"blockchain-node/logger" // BARU: Impor logger
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	// "github.com/ethereum/go-ethereum/params" // Tidak dibutuhkan secara langsung di sini, didapat dari interfaces.ChainConfigItf
)

// Blockchain defines the interface the EVM needs to access blockchain-specific data.
// core.Blockchain should implement this.
type Blockchain interface {
	GetConfig() interfaces.ChainConfigItf                     // Returns chain configuration
	GetBlockByNumber(number uint64) interfaces.BlockHeaderItf // Gets a block header by number
	// GetBlockByHash is not directly used by GetHashFn, GetHashFn uses GetBlockByNumber.
	// Jika GetHashFn diubah untuk menggunakan GetBlockByHash, maka uncomment dan implementasikan:
	// GetBlockByHash(hash common.Hash) interfaces.BlockHeaderItf
}

// EVM is a wrapper around the go-ethereum EVM.
type EVM struct {
	blockchain Blockchain
	vmConfig   vm.Config // vm.Config from go-ethereum
}

// NewEVM creates a new EVM instance.
func NewEVM(blockchain Blockchain) *EVM {
	// vm.Config can be initialized with specific parameters if needed,
	// e.g., Tracer, NoBaseFee, etc. For now, a default config is used.
	config := vm.Config{
		// Debug:     false, // Set true for debugging EVM execution
		// Tracer:    logger.NewTracer("EVM"), // Contoh jika Anda punya EVM logger kustom
		// NoBaseFee: true, // Set true jika chain Anda tidak menggunakan EIP-1559 BaseFee
	}

	return &EVM{
		blockchain: blockchain,
		vmConfig:   config,
	}
}

// ExecuteTransaction executes a transaction using the EVM.
// ctx *interfaces.ExecutionContext provides all necessary context for the execution.
func (e *EVM) ExecuteTransaction(ctx *interfaces.ExecutionContext) (*interfaces.ExecutionResult, error) {
	sdb := ctx.StateDB             // Your custom state.StateDB
	txItf := ctx.Transaction       // Your custom core.Transaction (implements interfaces.TransactionItf)
	headerItf := ctx.BlockHeader   // Your custom core.BlockHeader (implements interfaces.BlockHeaderItf)
	gasUsedPool := ctx.GasUsedPool // *big.Int, pointer to the block's cumulative gas used

	// stateAdapter bridges your custom StateDB to the vm.StateDB interface required by go-ethereum's EVM.
	stateAdapter := NewStateAdapter(sdb)

	var coinbase common.Address
	minerAddrBytes := headerItf.GetMiner() // This is [20]byte
	if minerAddrBytes != ([20]byte{}) {    // Check if it's not a zero address
		coinbase = common.BytesToAddress(minerAddrBytes[:])
	}

	// Populate BlockContext for the EVM
	blockContext := vm.BlockContext{
		CanTransfer: stateAdapter.CanTransfer, // Provided by StateAdapter
		Transfer:    stateAdapter.Transfer,    // Provided by StateAdapter
		GetHash:     e.GetHashFn(headerItf),   // Function to get historical block hashes
		Coinbase:    coinbase,
		BlockNumber: new(big.Int).SetUint64(headerItf.GetNumber()),
		Time:        new(big.Int).SetInt64(headerItf.GetTimestamp()), // PERBAIKAN: Time adalah *big.Int
		Difficulty:  new(big.Int).Set(headerItf.GetDifficulty()),     // Ensure this is a new big.Int
		GasLimit:    headerItf.GetGasLimit(),
		BaseFee:     nil, // Set to a value if your chain supports EIP-1559
		// Random: nil, // For PREVRANDAO, set if supporting The Merge and later forks
	}

	txFromBytes := txItf.GetFrom() // This is [20]byte
	txOrigin := common.BytesToAddress(txFromBytes[:])

	// Populate TxContext for the EVM
	txContext := vm.TxContext{
		Origin:   txOrigin,
		GasPrice: new(big.Int).Set(txItf.GetGasPrice()), // Ensure this is a new big.Int
		// Value:    txItf.GetValue(), // Value is passed directly to Call/Create
	}

	// Get chain rules (e.g., EIP activation blocks) from your blockchain's config
	chainRules := e.blockchain.GetConfig().ToEthChainConfig() // This returns *params.ChainConfig

	// Create a new EVM instance for this transaction execution
	evmInstance := vm.NewEVM(blockContext, txContext, stateAdapter, chainRules, e.vmConfig)

	var returnData []byte
	var vmError error
	var gasLeft uint64
	var createdContractAddress common.Address

	txData := txItf.GetData()
	txGasLimit := txItf.GetGasLimit()
	txValue := txItf.GetValue()

	senderRef := vm.AccountRef(txOrigin) // Sender's account reference

	if txItf.IsContractCreation() {
		// Execute contract creation
		returnData, createdContractAddress, gasLeft, vmError = evmInstance.Create(senderRef, txData, txGasLimit, txValue)
	} else {
		// Execute message call
		txToPtr := txItf.GetTo() // This is *[20]byte
		if txToPtr == nil {
			// This should ideally be caught by transaction validation before reaching here
			err := errors.New("transaction 'to' address is nil for non-contract creation")
			if gasUsedPool != nil { // Update gas pool even on this type of error
				gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit))
			}
			return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err}, err
		}
		toAddrBytes := *txToPtr // Dereference to [20]byte
		toAddr := common.BytesToAddress(toAddrBytes[:])
		returnData, gasLeft, vmError = evmInstance.Call(senderRef, toAddr, txData, txGasLimit, txValue)
	}

	// Prepare the execution result
	executionResult := &interfaces.ExecutionResult{
		Status:     1, // Assume success initially
		ReturnData: returnData,
		Logs:       []interfaces.Log{}, // Logs will be populated by core.Blockchain from stateDB
	}
	actualGasUsed := txGasLimit - gasLeft

	if vmError != nil {
		executionResult.Status = 0 // Mark as failed
		executionResult.Err = vmError
		logger.Warningf("EVM execution error for tx %x: %v. Gas used: %d", txItf.GetHash(), vmError, actualGasUsed)
	}
	executionResult.GasUsed = actualGasUsed

	if txItf.IsContractCreation() && executionResult.Status == 1 {
		if createdContractAddress != (common.Address{}) {
			var contractAddrBytes [20]byte
			copy(contractAddrBytes[:], createdContractAddress.Bytes())
			executionResult.ContractAddress = &contractAddrBytes
		} else {
			// It's possible for Create to succeed but return an empty address if the init code is empty or only returns.
			// This is not necessarily an error that should revert the state, but good to log.
			logger.Debugf("Contract creation for tx %x resulted in empty contract address (this might be intended if init code is empty).", txItf.GetHash())
		}
	}

	// Update the block's cumulative gas used pool
	if gasUsedPool != nil {
		gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(actualGasUsed))
	}

	return executionResult, executionResult.Err // Return VmError as the primary error status
}

// GetHashFn provides the GetHash function for the EVM's BlockContext.
// It allows the EVM to query for historical block hashes.
func (e *EVM) GetHashFn(currentProcessingHeader interfaces.BlockHeaderItf) vm.GetHashFunc {
	return func(blockNumToQuery uint64) common.Hash {
		// The EVM requests hashes of *previous* blocks relative to the block being processed.
		// blockNumToQuery is the block number for which the hash is requested.
		// currentProcessingHeader.GetNumber() is the number of the block currently being processed.
		if blockNumToQuery >= currentProcessingHeader.GetNumber() {
			// Requesting hash of current or future block relative to the one being processed.
			// Standard GetHashFn behavior is to return empty hash for such cases.
			return common.Hash{}
		}

		// Fetch the header for the requested block number.
		// e.blockchain is the Blockchain interface passed to NewEVM.
		headerItf := e.blockchain.GetBlockByNumber(blockNumToQuery)
		if headerItf != nil {
			hashBytes := headerItf.GetHash() // This is [32]byte
			return common.BytesToHash(hashBytes[:])
		}
		// Return empty hash if the block is not found (e.g., too old or chain reorged).
		return common.Hash{}
	}
}
