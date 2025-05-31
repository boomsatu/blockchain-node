package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blockchain-node/logger" // Pastikan logger Anda sudah bisa diakses di sini

	"github.com/spf13/viper"
)

type Config struct {
	// Node configuration
	DataDir string `mapstructure:"datadir"`
	Port    int    `mapstructure:"port"`    // Port P2P
	RPCPort int    `mapstructure:"rpcport"` // Port RPC
	RPCAddr string `mapstructure:"rpcaddr"`

	// Mining configuration
	Mining bool   `mapstructure:"mining"`
	Miner  string `mapstructure:"miner"`

	// Network configuration
	MaxPeers  int      `mapstructure:"maxpeers"`
	BootNodes []string `mapstructure:"bootnode"` // Sesuai tag di file config.go Anda
	EnableP2P bool     `mapstructure:"enable_p2p"`

	// Chain configuration
	ChainID       uint64 `mapstructure:"chainid"`
	BlockGasLimit uint64 `mapstructure:"blockgaslimit"`

	// Database configuration
	Cache   int `mapstructure:"cache"`
	Handles int `mapstructure:"handles"`

	// Logging configuration
	LogLevel  string `mapstructure:"log_level"`
	Verbosity int    `mapstructure:"verbosity"`

	// Security configuration
	EnableRateLimit bool          `mapstructure:"enable_rate_limit"`
	RateLimit       int           `mapstructure:"rate_limit"`
	RateLimitWindow time.Duration `mapstructure:"rate_limit_window"`

	// Performance configuration
	EnableCache       bool          `mapstructure:"enable_cache"`
	CacheSize         int           `mapstructure:"cache_size"`
	ConnectionTimeout time.Duration `mapstructure:"connection_timeout"`

	// Health check and Metrics configuration
	EnableHealth        bool          `mapstructure:"enable_health"`
	HealthPort          int           `mapstructure:"health_port"`
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`
	EnableMetrics       bool          `mapstructure:"enable_metrics"`
}

var defaultConfig = Config{
	DataDir:             "./data",    // Mengambil default dari file config.go Anda
	Port:                8080,        // Mengambil default dari file config.go Anda
	RPCPort:             8545,        // Mengambil default dari file config.go Anda
	RPCAddr:             "127.0.0.1", // Mengambil default dari file config.go Anda
	Mining:              false,
	Miner:               "",
	MaxPeers:            50,
	BootNodes:           []string{},
	EnableP2P:           true, // Default yang baik
	ChainID:             1337,
	BlockGasLimit:       8000000,
	Cache:               256,
	Handles:             256,
	LogLevel:            "info", // Default yang lebih eksplisit
	Verbosity:           3,      // Sesuai "info"
	EnableRateLimit:     true,
	RateLimit:           100,
	RateLimitWindow:     time.Minute,
	EnableCache:         true,
	CacheSize:           1000, // Default dari file config.go Anda
	ConnectionTimeout:   30 * time.Second,
	HealthCheckInterval: 30 * time.Second,
	EnableMetrics:       true,
	EnableHealth:        true, // Default yang baik
	HealthPort:          9545, // Port default untuk health (misalnya, RPCPort default + 1000)
}

// LoadConfig sekarang tidak menerima configPath.
// initConfig di cmd/root.go seharusnya sudah mengatur Viper.
func LoadConfig() (*Config, error) {
	config := defaultConfig // Mulai dengan default

	// Viper sudah membaca file konfigurasi (jika ada) di initConfig.
	// Sekarang kita hanya perlu melakukan Unmarshal.
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from Viper: %v", err)
	}

	// Logging untuk memverifikasi nilai yang dimuat.
	// Ini sangat penting untuk debugging.
	// Jika logger belum sepenuhnya siap, gunakan fmt.Fprintf(os.Stderr, ...)
	loadedConfigMsg := fmt.Sprintf("DEBUG: Effective Config Loaded in config.LoadConfig: "+
		"DataDir='%s', P2PPort=%d, RPCPort=%d, HealthPort=%d, Mining=%t, Miner='%s', ChainID=%d, LogLevel='%s', BootNodes=%v, EnableP2P=%t",
		config.DataDir, config.Port, config.RPCPort, config.HealthPort, config.Mining, config.Miner, config.ChainID, config.LogLevel, config.BootNodes, config.EnableP2P)

	// Gunakan logger jika sudah siap, jika tidak cetak ke Stderr untuk debugging awal.
	if logger.GetLogger() != nil { // Perlu cara untuk mengecek apakah logger sudah di-setup
		logger.Debug(loadedConfigMsg)
	} else {
		fmt.Fprintln(os.Stderr, loadedConfigMsg)
	}

	if err := validateAndCreateDirs(&config); err != nil {
		return nil, fmt.Errorf("config validation and directory creation failed: %v", err)
	}

	return &config, nil
}

func validateAndCreateDirs(config *Config) error {
	config.DataDir = strings.TrimSpace(config.DataDir) // Hapus spasi di awal/akhir
	if config.DataDir == "" {
		return fmt.Errorf("datadir cannot be empty")
	}
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory '%s': %v", config.DataDir, err)
	}

	chaindataDir := filepath.Join(config.DataDir, "chaindata")
	if err := os.MkdirAll(chaindataDir, 0755); err != nil {
		return fmt.Errorf("failed to create chaindata directory '%s': %v", chaindataDir, err)
	}

	walletDir := filepath.Join(config.DataDir, "wallet")
	if err := os.MkdirAll(walletDir, 0755); err != nil {
		return fmt.Errorf("failed to create wallet directory '%s': %v", walletDir, err)
	}

	p2pDir := filepath.Join(config.DataDir, "p2p")
	if err := os.MkdirAll(p2pDir, 0700); err != nil {
		return fmt.Errorf("failed to create p2p directory '%s': %v", p2pDir, err)
	}

	portsInUse := make(map[int]string)
	if config.EnableP2P {
		if config.Port <= 0 || config.Port > 65535 {
			return fmt.Errorf("invalid P2P port: %d", config.Port)
		}
		portsInUse[config.Port] = "P2P"
	}

	if config.RPCPort <= 0 || config.RPCPort > 65535 {
		return fmt.Errorf("invalid RPC port: %d", config.RPCPort)
	}
	if _, exists := portsInUse[config.RPCPort]; exists && config.EnableP2P {
		return fmt.Errorf("RPC port %d conflicts with P2P port %d", config.RPCPort, config.Port)
	}
	portsInUse[config.RPCPort] = "RPC"

	if config.EnableHealth {
		if config.HealthPort <= 0 || config.HealthPort > 65535 {
			return fmt.Errorf("invalid Health port: %d", config.HealthPort)
		}
		if _, exists := portsInUse[config.HealthPort]; exists {
			return fmt.Errorf("Health port %d conflicts with %s port", config.HealthPort, portsInUse[config.HealthPort])
		}
	}

	if config.MaxPeers <= 0 && config.EnableP2P {
		config.MaxPeers = defaultConfig.MaxPeers // Gunakan default jika invalid
	}
	if config.BlockGasLimit == 0 {
		config.BlockGasLimit = defaultConfig.BlockGasLimit
	}
	if config.Cache <= 0 {
		config.Cache = defaultConfig.Cache
	}
	if config.Handles <= 0 {
		config.Handles = defaultConfig.Handles
	}
	return nil
}

func (c *Config) GetLogLevel() logger.LogLevel {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return logger.DEBUG
	case "info":
		return logger.INFO
	case "warn", "warning":
		return logger.WARNING
	case "error":
		return logger.ERROR
	case "fatal":
		return logger.FATAL
	default:
		switch c.Verbosity {
		case 0, 1:
			return logger.ERROR
		case 2:
			return logger.WARNING
		case 3:
			return logger.INFO
		case 4, 5:
			return logger.DEBUG
		default:
			return logger.INFO
		}
	}
}

func (c *Config) IsMainnet() bool {
	return c.ChainID == 1
}

func (c *Config) IsTestnet() bool { // Contoh, ChainID testnet bisa berbeda
	return c.ChainID == 3 || c.ChainID == 4 || c.ChainID == 5 || c.ChainID == 11155111 // Sepolia
}

func (c *Config) GetDataSubDir(subdir string) string {
	return filepath.Join(c.DataDir, subdir)
}
