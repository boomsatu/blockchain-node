package cmd

import (
	"blockchain-node/config" // Impor config aplikasi
	"blockchain-node/consensus"
	"blockchain-node/core"
	"blockchain-node/crypto"
	"blockchain-node/execution" // Atau "blockchain-node/evm" jika Anda beralih
	"blockchain-node/health"
	"blockchain-node/logger"
	"blockchain-node/metrics"
	"blockchain-node/network"
	"blockchain-node/rpc"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"github.com/spf13/cobra"
	"github.com/spf13/viper" // Impor viper untuk mendapatkan nilai flag/config
)

var startNodeCmd = &cobra.Command{
	Use:   "startnode",
	Short: "Start the custom blockchain node",
	Long:  `Start the custom blockchain node with P2P networking, RPC server, and optional mining.`,
	RunE:  runStartNode, // Menggunakan RunE untuk error handling yang lebih baik
}

func init() {
	// Flag yang spesifik untuk 'startnode'
	// Nilai default diambil dari config.DefaultConfig agar konsisten
	startNodeCmd.Flags().Bool("mining", config.DefaultConfig.Mining, "Enable mining")
	startNodeCmd.Flags().String("miner", config.DefaultConfig.Miner, "Miner address for rewards (e.g., 0xYourAddress)")

	// Bind flag ini ke Viper agar bisa ditimpa oleh config file atau ENV var
	// Nama yang di-bind harus cocok dengan key di struct Config dan file config.yaml
	viper.BindPFlag("mining", startNodeCmd.Flags().Lookup("mining"))
	viper.BindPFlag("miner", startNodeCmd.Flags().Lookup("miner"))
}

func loadOrGenerateNodeKey(datadir string) (*ecdsa.PrivateKey, error) {
	p2pKeyDir := filepath.Join(datadir, "p2p")
	if err := os.MkdirAll(p2pKeyDir, 0700); err != nil { // 0700 untuk direktori kunci privat
		return nil, fmt.Errorf("failed to create p2p key directory %s: %v", p2pKeyDir, err)
	}
	keyfilePath := filepath.Join(p2pKeyDir, "node.key")

	keyBytes, err := os.ReadFile(keyfilePath)
	if err == nil {
		privKey, errPriv := crypto.ToECDSA(keyBytes) // Asumsi ToECDSA ada di package crypto Anda
		if errPriv == nil {
			logger.Infof("Loaded P2P node key from %s", keyfilePath)
			return privKey, nil
		}
		logger.Warningf("Failed to parse P2P node key from %s: %v. Generating new one.", keyfilePath, errPriv)
	} else if !os.IsNotExist(err) {
		// Hanya log warning jika errornya bukan karena file tidak ada
		logger.Warningf("Error reading P2P node key file %s: %v. Generating new one.", keyfilePath, err)
	}

	// GenerateEthKeyPair untuk kunci yang kompatibel dengan Ethereum (secp256k1)
	privKey, _, err := crypto.GenerateEthKeyPair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate P2P node key: %v", err)
	}
	// FromECDSA untuk serialisasi kunci privat
	if err := os.WriteFile(keyfilePath, crypto.FromECDSA(privKey), 0600); err != nil { // 0600 untuk file kunci privat
		return nil, fmt.Errorf("failed to save P2P node key to %s: %v", keyfilePath, err)
	}
	logger.Infof("Generated and saved new P2P node key to %s", keyfilePath)
	return privKey, nil
}

func getOutboundIP() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, address := range addrs {
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					logger.Debugf("Determined outbound IP from interfaces: %s", ipnet.IP.String())
					return ipnet.IP.String()
				}
			}
		}
	}
	// Fallback jika tidak ada IP non-loopback yang cocok ditemukan
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 2*time.Second) // Tambahkan timeout
	if err != nil {
		logger.Warningf("Could not determine outbound IP using DNS, defaulting to 127.0.0.1: %v", err)
		return "127.0.0.1"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	logger.Debugf("Determined outbound IP via UDP dial: %s", localAddr.IP.String())
	return localAddr.IP.String()
}

func runStartNode(cmd *cobra.Command, args []string) error {
	// cfgNodeApp adalah *config.Config dari package config aplikasi Anda
	cfgNodeApp, err := config.LoadConfig()
	if err != nil {
		// Tidak perlu logger.Fatal di sini, cukup return error
		// fmt.Fprintf(os.Stderr, "Failed to load application config: %v\n", err)
		return fmt.Errorf("failed to load application config: %v", err)
	}

	logger.SetLevel(cfgNodeApp.GetLogLevel())
	logger.Info("Starting Lumina Blockchain Node...")
	logger.Infof("Effective Application Configuration: DataDir=%s, P2PPort=%d, RPCPort=%d, HealthPort=%d, Mining=%t, Miner=%s, ChainID=%d, LogLevelString=%s, ForceGenesis=%t, GenesisFilePath='%s', BootNodes=%v",
		cfgNodeApp.DataDir, cfgNodeApp.Port, cfgNodeApp.RPCPort, cfgNodeApp.HealthPort, cfgNodeApp.Mining, cfgNodeApp.Miner, cfgNodeApp.ChainID, cfgNodeApp.LogLevel, cfgNodeApp.ForceGenesis, cfgNodeApp.GenesisFilePath, cfgNodeApp.BootNodes)

	nodeKey, err := loadOrGenerateNodeKey(cfgNodeApp.DataDir)
	if err != nil {
		// logger.Errorf sudah mencatat, jadi return error saja
		return fmt.Errorf("failed to load or generate P2P node key: %v", err)
	}

	publicKeyBytesUncompressed := crypto.FromECDSAPub(&nodeKey.PublicKey)
	var nodeIDHex string
	if len(publicKeyBytesUncompressed) == 65 && publicKeyBytesUncompressed[0] == 0x04 {
		nodeIDHex = hex.EncodeToString(publicKeyBytesUncompressed[1:])
	} else {
		logger.Errorf("P2P public key has unexpected format (length: %d, prefix: %x). Cannot derive valid Node ID.", len(publicKeyBytesUncompressed), publicKeyBytesUncompressed[0])
		return fmt.Errorf("P2P public key has unexpected format, cannot derive Node ID")
	}

	localIP := getOutboundIP()
	enodeURL := fmt.Sprintf("enode://%s@%s:%d", nodeIDHex, localIP, cfgNodeApp.Port)
	logger.Infof("Node Enode URL (best effort, check if behind NAT): %s", enodeURL)

	// blockchainCoreConfig adalah *core.Config
	blockchainCoreConfig := &core.Config{
		DataDir:       cfgNodeApp.DataDir,
		ChainID:       cfgNodeApp.ChainID,
		BlockGasLimit: cfgNodeApp.BlockGasLimit,
		// Jika Anda ingin core.Config juga tahu path genesis, tambahkan di sini:
		// GenesisFilePath: cfgNodeApp.GenesisFilePath,
	}
	// PERBAIKAN: Teruskan cfgNodeApp (config aplikasi) ke NewBlockchain sebagai argumen kedua
	blockchain, err := core.NewBlockchain(blockchainCoreConfig, cfgNodeApp)
	if err != nil {
		return fmt.Errorf("failed to initialize blockchain: %v", err)
	}
	defer func() {
		logger.Info("Closing blockchain...")
		if err := blockchain.Close(); err != nil {
			logger.Errorf("Error closing blockchain: %v", err)
		}
	}()

	consensusEngine := consensus.NewProofOfWork()
	blockchain.SetConsensus(consensusEngine)

	vm := execution.NewVirtualMachine()
	blockchain.SetVirtualMachine(vm)

	var healthChecker *health.HealthChecker
	if cfgNodeApp.EnableHealth {
		healthChecker = health.NewHealthChecker(blockchain, blockchain.GetDatabase())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	if cfgNodeApp.EnableP2P {
		p2pServer := network.NewServer(cfgNodeApp.Port, blockchain, nodeKey, enodeURL, cfgNodeApp.BootNodes)
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Infof("Starting P2P server on port %d", cfgNodeApp.Port)
			if err := p2pServer.Start(ctx); err != nil {
				if !strings.Contains(err.Error(), "use of closed network connection") && !errors.Is(err, context.Canceled) {
					logger.Errorf("P2P server error: %v", err)
				} else {
					logger.Info("P2P server shut down.")
				}
				cancel()
			}
		}()
	} else {
		logger.Info("P2P server is disabled via configuration.")
	}

	rpcConfig := &rpc.Config{
		Host: cfgNodeApp.RPCAddr,
		Port: cfgNodeApp.RPCPort,
	}
	rpcServer := rpc.NewServer(rpcConfig, blockchain)
	logger.Infof("Attempting to start RPC server on %s:%d", cfgNodeApp.RPCAddr, cfgNodeApp.RPCPort)
	if err := rpcServer.Start(); err != nil {
		logger.Errorf("Failed to start RPC server: %v", err)
		cancel()
		// return fmt.Errorf("failed to start RPC server: %v") // Bisa keluar jika RPC kritikal
	} else {
		logger.Info("RPC server started.")
		// RPC server Stop akan dipanggil saat shutdown
		defer func() {
			logger.Info("Stopping RPC server...")
			rpcServer.Stop()
			logger.Info("RPC server stopped.")
		}()
	}

	var healthServer *http.Server
	if cfgNodeApp.EnableHealth && healthChecker != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			healthAddr := fmt.Sprintf(":%d", cfgNodeApp.HealthPort)
			mux := http.NewServeMux()
			mux.HandleFunc("/health", healthChecker.HealthHandler)
			mux.HandleFunc("/ready", healthChecker.ReadinessHandler)
			if cfgNodeApp.EnableMetrics {
				mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
					metricsData := metrics.GetMetrics().ToMap()
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(metricsData); err != nil {
						logger.Errorf("Failed to encode metrics data: %v", err)
						http.Error(w, "Error encoding metrics", http.StatusInternalServerError)
					}
				})
			}
			healthServer = &http.Server{Addr: healthAddr, Handler: mux}
			logger.Infof("Starting health & metrics server on %s", healthAddr)

			serverErrCh := make(chan error, 1)
			go func() {
				if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					serverErrCh <- err
				}
				close(serverErrCh)
			}()

			select {
			case err := <-serverErrCh:
				if err != nil {
					logger.Errorf("Health server error: %v", err)
					cancel()
				}
			case <-ctx.Done():
				logger.Info("Context cancelled, shutting down health server...")
			}

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := healthServer.Shutdown(shutdownCtx); err != nil {
				logger.Errorf("Health server graceful shutdown error: %v", err)
			}
			logger.Info("Health & metrics server stopped.")
		}()
	}

	finalMiningEnabled := viper.GetBool("mining")
	finalMinerAddrStr := viper.GetString("miner")

	var minerInstance *core.Miner
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
					logger.Infof("Miner starting for address: %s", finalMinerAddrStr)
					minerInstance.Start()
					<-ctx.Done()
					logger.Info("Miner received shutdown signal, stopping...")
					minerInstance.Stop()
					logger.Info("Miner stopped.")
				}()
			}
		}
	} else {
		logger.Info("Mining is disabled.")
	}

	logger.Info("Lumina Blockchain Node started successfully. Press Ctrl+C to stop.")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case s := <-sigCh:
		logger.Infof("Received signal: %v, initiating shutdown...", s)
	case <-ctx.Done():
		logger.Info("Context cancelled (e.g., by service error), initiating shutdown...")
	}

	logger.Info("Shutting down services...")
	cancel()

	shutdownGracePeriod := 10 * time.Second
	logger.Infof("Waiting up to %v for services to shut down gracefully...", shutdownGracePeriod)

	doneWg := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneWg)
	}()

	select {
	case <-doneWg:
		logger.Info("All managed goroutines stopped gracefully.")
	case <-time.After(shutdownGracePeriod):
		logger.Warningf("Shutdown grace period of %v exceeded. Some services might not have stopped cleanly.", shutdownGracePeriod)
	}

	logger.Info("Lumina Blockchain Node shut down complete.")
	return nil
}
