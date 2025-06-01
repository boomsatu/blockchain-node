// File: evm/evm.go
package evm

import (
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm" // Impor utama untuk EVM
	"github.com/holiman/uint256"
)

// Blockchain mendefinisikan antarmuka yang dibutuhkan EVM untuk mengakses data spesifik blockchain.
// Antarmuka ini harus diimplementasikan oleh struktur Blockchain utama Anda.
type Blockchain interface {
	GetConfig() interfaces.ChainConfigItf                           // Mengembalikan konfigurasi chain (termasuk ChainID, hard forks)
	GetBlockByNumberForEVM(number uint64) interfaces.BlockHeaderItf // Mengambil header blok berdasarkan nomor untuk fungsi GetHash EVM
}

// EVM adalah pembungkus (wrapper) di sekitar EVM go-ethereum.
type EVM struct {
	blockchain Blockchain // Referensi ke implementasi Blockchain Anda
	vmConfig   vm.Config  // Konfigurasi untuk EVM (misalnya, untuk debugging, tracing)
}

// NewEVM membuat instance EVM baru.
func NewEVM(blockchain Blockchain) *EVM {
	// Konfigurasi vm.Config bisa disesuaikan.
	// Misalnya, untuk debugging atau tracing.
	config := vm.Config{
		// Debug: true, // Aktifkan jika Anda ingin output debug dari EVM
		// Tracer: logger.NewTracer("EVM_DETAIL"), // Gunakan tracer kustom jika perlu log EVM yang sangat detail
		// NoBaseFee: true, // Jika chain Anda tidak menggunakan EIP-1559 BaseFee (misalnya, chain PoW murni)
		// ExtraEips: []int{}, // Untuk mengaktifkan EIP tambahan jika diperlukan
	}
	return &EVM{
		blockchain: blockchain,
		vmConfig:   config,
	}
}

// ExecuteTransaction menjalankan transaksi menggunakan EVM.
func (e *EVM) ExecuteTransaction(ctx *interfaces.ExecutionContext) (*interfaces.ExecutionResult, error) {
	sdb := ctx.StateDB             // StateDB kustom Anda dari ExecutionContext
	txItf := ctx.Transaction       // Transaksi yang akan dieksekusi
	headerItf := ctx.BlockHeader   // Header blok saat ini
	gasUsedPool := ctx.GasUsedPool // Pool gas yang sudah terpakai di blok ini

	// Buat StateAdapter dari StateDB kustom Anda
	stateAdapter := NewStateAdapter(sdb)

	// Siapkan Coinbase (alamat miner)
	var coinbase common.Address
	minerAddrBytes := headerItf.GetMiner() // Dari BlockHeaderItf
	if minerAddrBytes != ([20]byte{}) {    // Pastikan tidak alamat nol
		coinbase = common.BytesToAddress(minerAddrBytes[:])
	} else {
		// Jika miner adalah alamat nol, EVM mungkin akan error atau berperilaku tidak terduga.
		// Sebaiknya pastikan header blok selalu memiliki miner yang valid.
		logger.Warningf("EVM.ExecuteTransaction: Coinbase (miner) address is zero for block %d. This might cause issues.", headerItf.GetNumber())
	}

	// Siapkan BlockContext untuk EVM
	// CanTransfer dan Transfer sekarang diambil dari stateAdapter
	blockContext := vm.BlockContext{
		CanTransfer: stateAdapter.CanTransfer, // Menggunakan implementasi dari StateAdapter
		Transfer:    stateAdapter.Transfer,    // Menggunakan implementasi dari StateAdapter
		GetHash:     e.GetHashFn(headerItf),   // Fungsi untuk mendapatkan hash blok sebelumnya
		Coinbase:    coinbase,
		BlockNumber: new(big.Int).SetUint64(headerItf.GetNumber()),
		Time:        uint64(headerItf.GetTimestamp()),            // Timestamp blok (uint64)
		Difficulty:  new(big.Int).Set(headerItf.GetDifficulty()), // Kesulitan blok
		GasLimit:    headerItf.GetGasLimit(),                     // Batas gas blok
		BaseFee:     nil,                                         // Untuk chain non-EIP-1559, BaseFee adalah nil. Jika chain Anda mendukung EIP-1559, ini harus diisi.
		// Random:      nil, // Untuk EIP-4399 (PREVRANDAO), jika didukung.
	}

	// Dapatkan ChainConfig dari implementasi Blockchain Anda
	chainRules := e.blockchain.GetConfig().ToEthChainConfig() // Mengembalikan *params.ChainConfig
	if chainRules == nil {
		err := errors.New("EVM.ExecuteTransaction: chainRules are nil, blockchain config might be missing or ToEthChainConfig() returns nil")
		logger.Errorf(err.Error())
		return &interfaces.ExecutionResult{GasUsed: txItf.GetGasLimit(), Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
	}

	// Buat instance EVM baru
	evmInstance := vm.NewEVM(blockContext, stateAdapter, chainRules, e.vmConfig)

	// Ambil snapshot state sebelum eksekusi untuk kemungkinan revert
	snapshotID := stateAdapter.Snapshot()

	var returnData []byte
	var vmError error
	var gasLeft uint64 // Gas yang tersisa setelah eksekusi
	var createdContractAddress common.Address

	txData := txItf.GetData()
	txGasLimit := txItf.GetGasLimit()

	// Konversi nilai transaksi ke *uint256.Int
	txValueUint256, overflow := uint256.FromBig(txItf.GetValue())
	if overflow {
		err := errors.New("transaction value overflowed uint256")
		logger.Errorf("EVM.ExecuteTransaction: %v for tx %x. Value: %s", err, txItf.GetHash(), txItf.GetValue().String())
		if gasUsedPool != nil {
			gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit)) // Semua gas dianggap terpakai
		}
		stateAdapter.RevertToSnapshot(snapshotID) // Kembalikan state
		return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
	}

	// Alamat pemanggil (sender)
	callerAddress := common.Address(txItf.GetFrom()) // Dari TransactionItf, sudah [20]byte

	// Jalankan transaksi
	if txItf.IsContractCreation() {
		// Untuk pembuatan kontrak
		// EVM.Create mengharapkan caller sebagai common.Address
		returnData, createdContractAddress, gasLeft, vmError = evmInstance.Create(
			callerAddress, // common.Address dari sender
			txData,
			txGasLimit,
			txValueUint256, // *uint256.Int
		)
	} else {
		// Untuk pemanggilan kontrak atau transfer ETH biasa
		txToPtr := txItf.GetTo() // *[20]byte
		if txToPtr == nil {
			// Ini seharusnya tidak terjadi jika IsContractCreation() false
			err := errors.New("transaction 'to' address is nil for non-contract creation")
			logger.Errorf("EVM.ExecuteTransaction: %v for tx %x", err, txItf.GetHash())
			if gasUsedPool != nil {
				gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(txGasLimit))
			}
			stateAdapter.RevertToSnapshot(snapshotID)
			return &interfaces.ExecutionResult{GasUsed: txGasLimit, Status: 0, Err: err, Logs: []*interfaces.Log{}}, err
		}
		toAddr := common.BytesToAddress((*txToPtr)[:]) // Konversi *[20]byte ke common.Address

		// EVM.Call mengharapkan caller dan toAddr sebagai common.Address
		returnData, gasLeft, vmError = evmInstance.Call(
			callerAddress, // common.Address dari sender
			toAddr,        // common.Address dari penerima
			txData,
			txGasLimit,
			txValueUint256, // *uint256.Int
		)
	}

	actualGasUsed := txGasLimit - gasLeft // Gas yang benar-benar digunakan
	finalLogs := []*interfaces.Log{}      // Inisialisasi slice untuk log

	if vmError != nil {
		logger.Warningf("EVM execution error for tx %x: %v. Gas used: %d. Reverting to snapshot %d.", txItf.GetHash(), vmError, actualGasUsed, snapshotID)
		stateAdapter.RevertToSnapshot(snapshotID) // Kembalikan state jika ada error dari EVM
	} else {
		// Jika tidak ada error dari EVM, ambil log yang dihasilkan dari StateDB
		// StateAdapter.AddLog akan dipanggil oleh EVM selama eksekusi.
		// Di sini, kita perlu mengambil log yang sudah dikumpulkan oleh StateDB kustom Anda.
		sdbLogs := sdb.GetLogs(txItf.GetHash()) // Asumsi GetLogs mengambil log untuk tx tertentu dari blockLogs StateDB
		for _, sdbLog := range sdbLogs {
			// Konversi customState.Log ke interfaces.Log
			topics := make([]common.Hash, len(sdbLog.Topics))
			for i, t := range sdbLog.Topics {
				topics[i] = common.BytesToHash(t[:])
			}
			finalLogs = append(finalLogs, &interfaces.Log{
				Address:     common.BytesToAddress(sdbLog.Address[:]),
				Topics:      topics,
				Data:        common.CopyBytes(sdbLog.Data),
				BlockNumber: sdbLog.BlockNumber,
				TxHash:      common.BytesToHash(sdbLog.TxHash[:]),
				TxIndex:     uint(sdbLog.TxIndex), // Konversi uint64 ke uint
				BlockHash:   common.BytesToHash(sdbLog.BlockHash[:]),
				Index:       uint(sdbLog.Index), // Konversi uint64 ke uint
				// Removed field tidak ada di interfaces.Log Anda, jadi tidak perlu di-set
			})
		}
		logger.Debugf("EVM execution successful for tx %x. Gas used: %d. Logs retrieved from StateDB: %d", txItf.GetHash(), actualGasUsed, len(finalLogs))
	}

	// Siapkan hasil eksekusi
	executionResult := &interfaces.ExecutionResult{
		GasUsed:    actualGasUsed,
		Status:     1, // Asumsi sukses jika tidak ada vmError
		ReturnData: returnData,
		Logs:       finalLogs,
		Err:        vmError, // Sertakan error dari EVM
	}

	if vmError != nil {
		executionResult.Status = 0 // Set status gagal jika ada error dari EVM
	}

	// Jika pembuatan kontrak berhasil, set alamat kontrak yang dibuat
	if txItf.IsContractCreation() && executionResult.Status == 1 {
		if createdContractAddress != (common.Address{}) { // Pastikan alamat tidak nol
			var contractAddrBytes [20]byte
			copy(contractAddrBytes[:], createdContractAddress.Bytes())
			executionResult.ContractAddress = &contractAddrBytes
		} else {
			// Ini bisa terjadi jika EVM.Create mengembalikan alamat nol meskipun tidak ada error eksplisit
			// (misalnya, jika kehabisan gas selama deployment tapi tidak dianggap error fatal oleh EVM).
			logger.Debugf("Contract creation for tx %x resulted in empty contract address, vmError: %v. Status: %d", txItf.GetHash(), vmError, executionResult.Status)
			// Jika status masih 1 tapi alamat kontrak nol, ini mungkin perlu ditangani sebagai kegagalan pembuatan kontrak.
			if executionResult.Status == 1 { // Jika status masih sukses tapi alamat kontrak nol
				// executionResult.Status = 0 // Anda bisa memilih untuk menandainya sebagai gagal di sini.
				// executionResult.Err = errors.New("contract creation resulted in zero address")
			}
		}
	}

	// Tambahkan gas yang digunakan ke pool gas blok
	if gasUsedPool != nil {
		gasUsedPool.Add(gasUsedPool, new(big.Int).SetUint64(actualGasUsed))
	}

	return executionResult, vmError // Kembalikan juga vmError untuk penanganan di level blockchain
}

// GetHashFn mengembalikan fungsi vm.GetHashFunc yang digunakan EVM untuk mendapatkan hash blok sebelumnya.
func (e *EVM) GetHashFn(currentProcessingHeader interfaces.BlockHeaderItf) vm.GetHashFunc {
	return func(blockNumToQuery uint64) common.Hash {
		// Logika untuk mencegah query ke blok masa depan atau blok saat ini
		if blockNumToQuery >= currentProcessingHeader.GetNumber() {
			// EVM seharusnya tidak meminta hash blok saat ini atau masa depan.
			// Mengembalikan hash nol jika ini terjadi.
			logger.Warningf("GetHashFn called for block number %d which is >= current processing block number %d. Returning zero hash.", blockNumToQuery, currentProcessingHeader.GetNumber())
			return common.Hash{}
		}

		// Ambil header blok dari implementasi Blockchain Anda
		headerItf := e.blockchain.GetBlockByNumberForEVM(blockNumToQuery)
		if headerItf != nil {
			hashBytes := headerItf.GetHash() // Dari BlockHeaderItf
			return common.BytesToHash(hashBytes[:])
		}
		// Jika blok tidak ditemukan (misalnya, di luar jangkauan chain yang diketahui), kembalikan hash nol.
		logger.Warningf("GetHashFn: Block header for number %d not found. Returning zero hash.", blockNumToQuery)
		return common.Hash{}
	}
}

// Pastikan EVM mengimplementasikan interfaces.VirtualMachine
var _ interfaces.VirtualMachine = (*EVM)(nil)
