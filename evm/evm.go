package evm

import (
	"blockchain-node/interfaces"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm" // Impor vm untuk vm.ExecutionResult
)

type Blockchain interface {
	GetConfig() interfaces.ChainConfigItf
	GetBlockByHash(hash common.Hash) interfaces.BlockHeaderItf
	GetBlockByNumber(number uint64) interfaces.BlockHeaderItf
}

type EVM struct {
	blockchain Blockchain
	vmConfig   vm.Config // Ini adalah vm.Config dari go-ethereum
}

func NewEVM(blockchain Blockchain) *EVM {
	chainConfigProvider := blockchain.GetConfig()
	chainConfigProvider.ToEthChainConfig()

	// Inisialisasi vm.Config
	config := vm.Config{
		Tracer: nil,
		// ChainConfig di vm.Config tidak ada lagi di versi go-ethereum yang lebih baru,
		// itu diteruskan langsung ke vm.NewEVM.
		// Jadi, kita bisa kosongkan atau isi field lain jika ada.
		// GasTable: params.MainnetGasTable, // Contoh, sesuaikan dengan chain Anda
	}

	return &EVM{
		blockchain: blockchain,
		vmConfig:   config, // Gunakan vm.Config yang sudah diinisialisasi
	}
}

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
		CanTransfer: stateAdapter.CanTransfer, // Sudah benar
		Transfer:    stateAdapter.Transfer,    // Sudah benar
		GetHash:     e.GetHashFn(headerItf),
		Coinbase:    coinbase,
		BlockNumber: new(big.Int).SetUint64(headerItf.GetNumber()),
		Time:        uint64(headerItf.GetTimestamp()),
		Difficulty:  new(big.Int).Set(headerItf.GetDifficulty()),
		GasLimit:    headerItf.GetGasLimit(),
		BaseFee:     nil,
	}

	txFrom := txItf.GetFrom()
	txGasPrice := txItf.GetGasPrice()

	txContext := vm.TxContext{
		Origin:   common.BytesToAddress(txFrom[:]),
		GasPrice: new(big.Int).Set(txGasPrice),
	}

	// Dapatkan ChainConfig dari provider yang ada di e.blockchain
	chainRules := e.blockchain.GetConfig().ToEthChainConfig()

	// vm.NewEVM(blockCtx BlockContext, txCtx TxContext, statedb StateDB, chainRules *params.ChainConfig, config Config)
	evmInstance := vm.NewEVM(blockContext, txContext, stateAdapter, chainRules, e.vmConfig) // stateAdapter implements vm.StateDB

	var returnData []byte
	var VmError error
	var gasLeft uint64
	var createdContractAddress common.Address // Untuk hasil dari Create

	//nonceForContractCreation := stateAdapter.GetNonce(common.BytesToAddress(txFrom[:]))

	txData := txItf.GetData()
	txGasLimit := txItf.GetGasLimit()
	txValue := txItf.GetValue()

	if txItf.IsContractCreation() {
		// Create mengembalikan: ret []byte, contractAddr common.Address, gasLeft uint64, err error
		returnData, createdContractAddress, gasLeft, VmError = evmInstance.Create(vm.AccountRef(common.BytesToAddress(txFrom[:])), txData, txGasLimit, txValue)
	} else {
		txToPtr := txItf.GetTo()
		if txToPtr == nil {
			err := errors.New("transaction 'to' address is nil for non-contract creation")
			if gasUsedPool != nil {
				gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit))
			}
			return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err}, err
		}
		toAddr := common.BytesToAddress(txToPtr[:])
		// Call mengembalikan: ret []byte, gasLeft uint64, err error
		returnData, gasLeft, VmError = evmInstance.Call(vm.AccountRef(common.BytesToAddress(txFrom[:])), toAddr, txData, txGasLimit, txValue)
	}

	executionResult := &interfaces.ExecutionResult{
		Status:     1, // Default sukses
		ReturnData: returnData,
	}
	actualGasUsed := txGasLimit - gasLeft // Gas yang terpakai adalah gas limit awal dikurangi sisa gas

	if VmError != nil {
		executionResult.Status = 0
		executionResult.Err = VmError
		// Jika error (revert, out of gas), EVM sudah menghitung gas yang terpakai hingga error.
		// Jadi, gasLeft akan merefleksikan sisa gas setelah error.
		// Jika errornya adalah OutOfGas, gasLeft akan 0, sehingga actualGasUsed = txGasLimit.
		// Jika revert dengan sisa gas, gasLeft akan > 0.
	}
	executionResult.GasUsed = actualGasUsed

	// Ambil log dari stateAdapter setelah eksekusi
	// (StateAdapter.AddLog dipanggil oleh EVM secara internal)
	// Kita perlu cara untuk mengambil log yang terkumpul di stateAdapter/sdb untuk transaksi ini.
	// Ini mungkin memerlukan modifikasi di StateDB atau StateAdapter
	// Untuk sementara, kita asumsikan EVM tidak menghasilkan log di vmResult untuk Create/Call secara langsung,
	// tapi melalui stateDB.AddLog. Jadi, kita ambil dari sdb setelah eksekusi.
	// Namun, vm.ExecutionResult (jika itu yang dimaksud `vmResult` sebelumnya) memang memiliki field Logs.
	// Karena kita tidak menangkap vm.ExecutionResult lagi, kita perlu cara lain.
	// Mari kita asumsikan vm.Logs() bisa dipanggil pada evmInstance setelah eksekusi, atau dari stateAdapter.
	// Skenario umum: EVM akan memanggil stateDB.AddLog(). Log ini perlu dikumpulkan.

	// Untuk sekarang, kita asumsikan ExecutionResult akan diisi dari Logs yang ada di StateDB (yang dipopulasi oleh EVM)
	// Ini berarti stateDB perlu metode GetLogsForCurrentTransaction() atau serupa,
	// atau kita kumpulkan log di ExecutionContext.
	// Untuk simplifikasi, kita akan kosongkan log di sini dan biarkan diisi di core/blockchain.go jika perlu.
	executionResult.Logs = []interfaces.Log{} // Placeholder, perlu mekanisme pengumpulan log yang benar

	if txItf.IsContractCreation() && executionResult.Status == 1 {
		// Gunakan createdContractAddress yang dikembalikan oleh evmInstance.Create
		if createdContractAddress != (common.Address{}) {
			var contractAddrBytes [20]byte
			copy(contractAddrBytes[:], createdContractAddress.Bytes())
			executionResult.ContractAddress = &contractAddrBytes
		} else {
			// Jika Create berhasil tapi tidak ada alamat kontrak (kasus aneh), log error
			executionResult.Status = 0
			executionResult.Err = errors.New("contract creation successful but no contract address returned")
		}
	}

	if gasUsedPool != nil {
		gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(actualGasUsed))
	}

	return executionResult, executionResult.Err
}

func (e *EVM) GetHashFn(currentHeader interfaces.BlockHeaderItf) vm.GetHashFunc {
	return func(n uint64) common.Hash {
		if n >= currentHeader.GetNumber() {
			return common.Hash{}
		}
		headerItf := e.blockchain.GetBlockByNumber(n)
		if headerItf != nil {
			hashBytes := headerItf.GetHash()
			return common.BytesToHash(hashBytes[:])
		}
		return common.Hash{}
	}
}

func commonHashesToByte32Array(hashes []common.Hash) [][32]byte {
	result := make([][32]byte, len(hashes))
	for i, h := range hashes {
		result[i] = [32]byte(h)
	}
	return result
}
