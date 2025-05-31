package cmd

import (
	"blockchain-node/config"
	"blockchain-node/consensus"
	"blockchain-node/core"
	"blockchain-node/crypto"
	"blockchain-node/execution"
	"blockchain-node/health"
	"blockchain-node/logger"
	"blockchain-node/metrics"
	"blockchain-node/network"
	"blockchain-node/rpc"

	// "blockchain-node/security" // Komentari jika tidak digunakan atau menyebabkan error
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	// Hapus atau ganti impor ini jika menyebabkan masalah dan tidak esensial untuk Node ID Anda
	// "github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/spf13/cobra"
)

var startNodeCmd = &cobra.Command{
	Use:   "startnode",
	Short: "Start the custom blockchain node",
	Long:  `Start the custom blockchain node with P2P networking, RPC server, and optional mining.`,
	RunE:  runStartNode,
}

func loadOrGenerateNodeKey(datadir string) (*ecdsa.PrivateKey, error) {
	// Pastikan direktori datadir ada, atau setidaknya direktori untuk nodekey
	p2pKeyDir := filepath.Join(datadir, "p2p") // Simpan di dalam subdirektori p2p
	if err := os.MkdirAll(p2pKeyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create p2p key directory %s: %v", p2pKeyDir, err)
	}
	keyfilePath := filepath.Join(p2pKeyDir, "node.key")

	keyBytes, err := os.ReadFile(keyfilePath)
	if err == nil {
		privKey, errPriv := crypto.ToECDSA(keyBytes)
		if errPriv == nil {
			logger.Infof("Loaded P2P node key from %s", keyfilePath)
			return privKey, nil
		}
		logger.Warningf("Failed to load P2P node key from %s: %v. Generating new one.", keyfilePath, errPriv)
	} else if !os.IsNotExist(err) {
		logger.Warningf("Error reading P2P node key file %s: %v. Generating new one.", keyfilePath, err)
	}

	privKey, _, err := crypto.GenerateEthKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate P2P node key: %v", err)
	}
	if err := os.WriteFile(keyfilePath, crypto.FromECDSA(privKey), 0600); err != nil {
		return nil, fmt.Errorf("failed to save P2P node key to %s: %v", keyfilePath, err)
	}
	logger.Infof("Generated and saved new P2P node key to %s", keyfilePath)
	return privKey, nil
}

// Helper untuk mendapatkan alamat IP non-loopback pertama yang ditemukan
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80") // Tidak benar-benar mengirim data
	if err != nil {
		logger.Warningf("Could not determine outbound IP, defaulting to 127.0.0.1: %v", err)
		return "127.0.0.1"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func runStartNode(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	logger.SetLevel(cfg.GetLogLevel())
	logger.Info("Starting custom blockchain node...")
	logger.Infof("Effective Configuration: DataDir=%s, P2PPort=%d, RPCPort=%d, HealthPort=%d, Mining=%t, Miner=%s, ChainID=%d, LogLevelString=%s",
		cfg.DataDir, cfg.Port, cfg.RPCPort, cfg.HealthPort, cfg.Mining, cfg.Miner, cfg.ChainID, cfg.LogLevel)

	nodeKey, err := loadOrGenerateNodeKey(cfg.DataDir)
	if err != nil {
		// logger.Fatalf sudah memanggil os.Exit(1), jadi return err tidak akan tercapai
		// Lebih baik log error dan return error agar bisa ditangani di main.go
		logger.Errorf("Failed to load or generate P2P node key: %v", err)
		return fmt.Errorf("failed to load or generate P2P node key: %v", err)
	}

	// Membuat Node ID dari public key P2P
	// Public key ECDSA (kurva secp256k1) adalah 64 byte (X dan Y concatenated).
	// Node ID adalah representasi heksadesimal dari public key ini.
	// crypto.FromECDSAPub mengembalikan public key dalam format uncompressed (0x04 + X + Y)
	publicKeyBytesUncompressed := crypto.FromECDSAPub(&nodeKey.PublicKey)
	var nodeIDHex string
	if len(publicKeyBytesUncompressed) == 65 && publicKeyBytesUncompressed[0] == 0x04 {
		nodeIDHex = hex.EncodeToString(publicKeyBytesUncompressed[1:]) // Ambil X dan Y (64 bytes)
	} else {
		// Fallback jika format tidak terduga, ini seharusnya tidak terjadi dengan GenerateEthKeyPair
		logger.Warningf("P2P public key has unexpected format/length: %d. Enode URL might be incorrect.", len(publicKeyBytesUncompressed))
		// Sebagai fallback yang sangat sederhana, kita bisa hash public key, tapi ini bukan Node ID standar.
		// Untuk sekarang, kita akan biarkan nodeIDHex kosong atau beri nilai placeholder jika error.
		// Lebih baik memastikan crypto.FromECDSAPub menghasilkan format yang benar.
		nodeIDHex = hex.EncodeToString(crypto.Keccak256(publicKeyBytesUncompressed)[:32]) // Ini BUKAN Node ID standar Ethereum
	}

	localIP := getOutboundIP() // Mencoba mendapatkan IP yang bisa dijangkau
	if cfg.RPCAddr != "0.0.0.0" && cfg.RPCAddr != "127.0.0.1" && cfg.RPCAddr != "localhost" {
		// Jika RPCAddr diset ke IP spesifik, gunakan itu untuk enode jika relevan
		// Namun, untuk P2P, IP yang bisa dijangkau dari luar lebih penting.
		// localIP = cfg.RPCAddr // Pertimbangkan ini jika RPCAddr adalah IP publik Anda
	}

	enodeURL := fmt.Sprintf("enode://%s@%s:%d", nodeIDHex, localIP, cfg.Port)
	logger.Infof("Node Enode URL: %s", enodeURL)

	blockchainConfig := &core.Config{
		DataDir:       cfg.DataDir,
		ChainID:       cfg.ChainID,
		BlockGasLimit: cfg.BlockGasLimit,
	}
	blockchain, err := core.NewBlockchain(blockchainConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize blockchain: %v", err)
	}
	defer func() {
		logger.Info("Closing blockchain...")
		if err := blockchain.Close(); err != nil {
			logger.Errorf("Failed to close blockchain: %v", err)
		}
	}()

	consensusEngine := consensus.NewProofOfWork()
	blockchain.SetConsensus(consensusEngine)

	vm := execution.NewVirtualMachine()
	blockchain.SetVirtualMachine(vm)

	var healthChecker *health.HealthChecker
	if cfg.EnableHealth {
		healthChecker = health.NewHealthChecker(blockchain, blockchain.GetDatabase())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	if cfg.EnableP2P {
		p2pServer := network.NewServer(cfg.Port, blockchain, nodeKey, enodeURL, cfg.BootNodes)
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Infof("Starting P2P server on port %d", cfg.Port)
			if err := p2pServer.Start(ctx); err != nil {
				logger.Errorf("P2P server error: %v", err)
				cancel()
			}
		}()
	} else {
		logger.Info("P2P server is disabled via configuration.")
	}

	rpcConfig := &rpc.Config{
		Host: cfg.RPCAddr,
		Port: cfg.RPCPort,
	}
	rpcServer := rpc.NewServer(rpcConfig, blockchain)
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Infof("Starting RPC server on %s:%d", cfg.RPCAddr, cfg.RPCPort)
		if err := rpcServer.Start(); err != nil {
			logger.Errorf("RPC server error: %v", err)
			cancel()
		}
	}()

	var healthServer *http.Server
	if cfg.EnableHealth && healthChecker != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			healthPort := cfg.HealthPort
			mux := http.NewServeMux()
			mux.HandleFunc("/health", healthChecker.HealthHandler)
			mux.HandleFunc("/ready", healthChecker.ReadinessHandler)
			if cfg.EnableMetrics {
				mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
					metricsData := metrics.GetMetrics().ToMap()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(metricsData)
				})
			}
			healthServer = &http.Server{Addr: fmt.Sprintf(":%d", healthPort), Handler: mux}
			logger.Infof("Starting health & metrics server on port %d", healthPort)

			serverErrCh := make(chan error, 1)
			go func() {
				serverErrCh <- healthServer.ListenAndServe()
			}()

			select {
			case err := <-serverErrCh:
				if err != nil && err != http.ErrServerClosed {
					logger.Errorf("Health server error: %v", err)
				}
			case <-ctx.Done():
				// Konteks dibatalkan, mulai shutdown server
			}

			logger.Info("Shutting down health & metrics server...")
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := healthServer.Shutdown(shutdownCtx); err != nil {
				logger.Errorf("Health server graceful shutdown error: %v", err)
			}
			logger.Info("Health & metrics server stopped.")
		}()
	}

	miningEnabledByFlag, _ := cmd.Flags().GetBool("mining")
	minerAddrStrByFlag, _ := cmd.Flags().GetString("miner")

	finalMiningEnabled := cfg.Mining
	if cmd.Flags().Changed("mining") {
		finalMiningEnabled = miningEnabledByFlag
	}
	finalMinerAddrStr := cfg.Miner
	if cmd.Flags().Changed("miner") {
		finalMinerAddrStr = minerAddrStrByFlag
	}

	var minerInstance *core.Miner // Deklarasikan di sini agar bisa di-Stop
	if finalMiningEnabled {
		if finalMinerAddrStr == "" {
			logger.Warning("Mining enabled but no miner address specified. Mining will not start.")
		} else {
			addrBytes, errHex := hex.DecodeString(strings.TrimPrefix(finalMinerAddrStr, "0x"))
			if errHex != nil || len(addrBytes) != 20 {
				logger.Errorf("Invalid miner address format: %s. Error: %v. Mining will not start.", finalMinerAddrStr, errHex)
			} else {
				var minerAddr20 [20]byte
				copy(minerAddr20[:], addrBytes)
				minerInstance = core.NewMiner(blockchain, minerAddr20, consensusEngine)
				wg.Add(1)
				go func() {
					defer wg.Done()
					logger.Infof("Starting miner with address: %s", finalMinerAddrStr)
					minerInstance.Start() // Start() harus non-blocking atau dijalankan di goroutine sendiri oleh Start()
					<-ctx.Done()
					logger.Info("Received stop signal for miner, stopping...")
					minerInstance.Stop()
				}()
			}
		}
	} else {
		logger.Info("Mining is disabled.")
	}

	logger.Info("Custom blockchain node started successfully. Press Ctrl+C to stop.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sigCh:
		logger.Infof("Received signal: %v, initiating shutdown...", s)
	case <-ctx.Done():
		logger.Info("Context cancelled by other service error, initiating shutdown...")
	}

	logger.Info("Broadcasting shutdown signal to all services...")
	cancel() // Signal semua goroutine untuk berhenti

	// Beri waktu untuk goroutine selesai
	shutdownCompleted := make(chan struct{})
	go func() {
		wg.Wait() // Tunggu semua goroutine yang di-Add ke wg selesai
		close(shutdownCompleted)
	}()

	select {
	case <-shutdownCompleted:
		logger.Info("All services stopped gracefully.")
	case <-time.After(10 * time.Second):
		logger.Warning("Timeout waiting for services to stop. Forcing exit.")
	}

	logger.Info("Custom blockchain node stopped.")
	return nil
}
