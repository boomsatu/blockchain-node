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

// Config struct holds all configuration for the application.
// Tags are used by viper to map ENV variables and config file keys.
type Config struct {
	// Node configuration
	DataDir string `mapstructure:"datadir"`
	Port    int    `mapstructure:"port"`    // P2P port
	RPCPort int    `mapstructure:"rpcport"` // RPC port
	RPCAddr string `mapstructure:"rpcaddr"`

	// Mining configuration
	Mining bool   `mapstructure:"mining"`
	Miner  string `mapstructure:"miner"`

	// Network configuration
	MaxPeers  int      `mapstructure:"maxpeers"`
	BootNodes []string `mapstructure:"bootnode"`
	EnableP2P bool     `mapstructure:"enable_p2p"`

	// Chain configuration
	ChainID       uint64 `mapstructure:"chainid"`
	BlockGasLimit uint64 `mapstructure:"blockgaslimit"`

	// Database configuration
	Cache   int `mapstructure:"cache"`   // Cache size for LevelDB (MB)
	Handles int `mapstructure:"handles"` // Number of open file handles for LevelDB

	// Logging configuration
	LogLevel  string `mapstructure:"log_level"` // e.g., "debug", "info", "warn", "error"
	Verbosity int    `mapstructure:"verbosity"` // Alternative to LogLevel, 0-5

	// Security configuration
	EnableRateLimit bool          `mapstructure:"enable_rate_limit"`
	RateLimit       int           `mapstructure:"rate_limit"`        // Requests per window
	RateLimitWindow time.Duration `mapstructure:"rate_limit_window"` // e.g., "1m", "1h"

	// Performance configuration
	EnableCache       bool          `mapstructure:"enable_cache"` // General in-memory cache for app data
	CacheSize         int           `mapstructure:"cache_size"`   // Number of items for general cache
	ConnectionTimeout time.Duration `mapstructure:"connection_timeout"`

	// Health check and Metrics configuration
	EnableHealth        bool          `mapstructure:"enable_health"`
	HealthPort          int           `mapstructure:"health_port"`
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`
	EnableMetrics       bool          `mapstructure:"enable_metrics"`

	// Genesis configuration
	ForceGenesis    bool   `mapstructure:"forcegenesis"`    // Force creation of a new genesis block
	GenesisFilePath string `mapstructure:"genesisfilepath"` // Path to the genesis.json file
}

// defaultConfig holds the unexported default configuration values.
var defaultConfig = Config{
	DataDir:             "./data_node_default_config",
	Port:                30303,
	RPCPort:             8545,
	RPCAddr:             "0.0.0.0",
	Mining:              false,
	Miner:               "",
	MaxPeers:            50,
	BootNodes:           []string{},
	EnableP2P:           true,
	ChainID:             1337,
	BlockGasLimit:       8000000,
	Cache:               256,
	Handles:             512,
	LogLevel:            "info",
	Verbosity:           3,
	EnableRateLimit:     true,
	RateLimit:           100,
	RateLimitWindow:     time.Minute,
	EnableCache:         true,
	CacheSize:           10000,
	ConnectionTimeout:   30 * time.Second,
	EnableHealth:        true,
	HealthPort:          9545,
	HealthCheckInterval: 30 * time.Second,
	EnableMetrics:       true,
	ForceGenesis:        false,
	GenesisFilePath:     "./genesis.json",
}

// DefaultConfig is an exported version of defaultConfig, allowing other packages
// to access the default values, for example, when setting up CLI flags.
var DefaultConfig = defaultConfig

// LoadConfig loads configuration from file, environment variables, and flags.
func LoadConfig() (*Config, error) {
	// Start with a copy of the exported DefaultConfig.
	// Viper will then override these values based on file, ENV, and flags.
	currentConfig := DefaultConfig

	if err := viper.Unmarshal(&currentConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from Viper: %v", err)
	}

	loadedConfigMsg := fmt.Sprintf("DEBUG: Effective Config Loaded in config.LoadConfig: "+
		"DataDir='%s', P2PPort=%d, RPCPort=%d, HealthPort=%d, Mining=%t, Miner='%s', ChainID=%d, LogLevel='%s', BootNodes=%v, EnableP2P=%t, ForceGenesis=%t, GenesisFilePath='%s'",
		currentConfig.DataDir, currentConfig.Port, currentConfig.RPCPort, currentConfig.HealthPort, currentConfig.Mining, currentConfig.Miner, currentConfig.ChainID, currentConfig.LogLevel, currentConfig.BootNodes, currentConfig.EnableP2P, currentConfig.ForceGenesis, currentConfig.GenesisFilePath)

	if logger.GetLogger() != nil {
		logger.Debug(loadedConfigMsg)
	} else {
		fmt.Fprintln(os.Stderr, loadedConfigMsg)
	}

	if err := validateAndCreateDirs(&currentConfig); err != nil {
		return nil, fmt.Errorf("config validation and directory creation failed: %v", err)
	}

	return &currentConfig, nil
}

func validateAndCreateDirs(config *Config) error {
	config.DataDir = strings.TrimSpace(config.DataDir)
	if config.DataDir == "" {
		return fmt.Errorf("datadir cannot be empty")
	}
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory '%s': %v", config.DataDir, err)
	}

	subDirs := []string{"chaindata", "wallet", "p2p"}
	for _, dirName := range subDirs {
		dirPath := filepath.Join(config.DataDir, dirName)
		perm := os.FileMode(0755)
		if dirName == "p2p" { // p2p directory for node key might need stricter permissions
			perm = 0700
		}
		if err := os.MkdirAll(dirPath, perm); err != nil {
			return fmt.Errorf("failed to create subdirectory '%s' in '%s': %v", dirName, config.DataDir, err)
		}
	}

	config.GenesisFilePath = strings.TrimSpace(config.GenesisFilePath)
	if config.GenesisFilePath == "" {
		config.GenesisFilePath = DefaultConfig.GenesisFilePath // Use exported default
		logger.Warningf("GenesisFilePath is empty, using default: %s", config.GenesisFilePath)
	}
	absGenesisPath, err := filepath.Abs(config.GenesisFilePath)
	if err != nil {
		logger.Warningf("Could not determine absolute path for genesis file '%s': %v. Proceeding with relative path.", config.GenesisFilePath, err)
	} else {
		if _, statErr := os.Stat(absGenesisPath); os.IsNotExist(statErr) {
			logger.Warningf("Genesis file specified at '%s' (resolved to '%s') does not appear to exist. Node might fail to start if it needs to create a new genesis or validate against it.", config.GenesisFilePath, absGenesisPath)
		} else if statErr != nil {
			logger.Warningf("Error checking status of genesis file '%s' (resolved to '%s'): %v.", config.GenesisFilePath, absGenesisPath, statErr)
		}
	}

	portsInUse := make(map[int]string)
	if config.EnableP2P {
		if config.Port <= 0 || config.Port > 65535 {
			return fmt.Errorf("invalid P2P port: %d. Must be between 1 and 65535", config.Port)
		}
		portsInUse[config.Port] = "P2P"
	}

	if config.RPCPort <= 0 || config.RPCPort > 65535 {
		return fmt.Errorf("invalid RPC port: %d. Must be between 1 and 65535", config.RPCPort)
	}
	if conflictService, exists := portsInUse[config.RPCPort]; exists && config.EnableP2P { // Check conflict only if P2P is enabled
		return fmt.Errorf("RPC port %d conflicts with %s port %d", config.RPCPort, conflictService, config.RPCPort)
	}
	portsInUse[config.RPCPort] = "RPC"

	if config.EnableHealth {
		if config.HealthPort <= 0 || config.HealthPort > 65535 {
			return fmt.Errorf("invalid Health port: %d. Must be between 1 and 65535", config.HealthPort)
		}
		if conflictService, exists := portsInUse[config.HealthPort]; exists {
			return fmt.Errorf("Health port %d conflicts with %s port %d", config.HealthPort, conflictService, config.HealthPort)
		}
	}

	if config.MaxPeers <= 0 && config.EnableP2P {
		logger.Warningf("MaxPeers is invalid (%d), using default: %d", config.MaxPeers, DefaultConfig.MaxPeers)
		config.MaxPeers = DefaultConfig.MaxPeers
	}
	if config.BlockGasLimit == 0 {
		logger.Warningf("BlockGasLimit is 0, using default: %d", DefaultConfig.BlockGasLimit)
		config.BlockGasLimit = DefaultConfig.BlockGasLimit
	}
	if config.Cache <= 0 {
		logger.Warningf("LevelDB Cache size is invalid (%d MB), using default: %d MB", config.Cache, DefaultConfig.Cache)
		config.Cache = DefaultConfig.Cache
	}
	if config.Handles <= 0 {
		logger.Warningf("LevelDB Handles count is invalid (%d), using default: %d", config.Handles, DefaultConfig.Handles)
		config.Handles = DefaultConfig.Handles
	}
	if config.CacheSize <= 0 && config.EnableCache {
		logger.Warningf("General CacheSize is invalid (%d items), using default: %d items", config.CacheSize, DefaultConfig.CacheSize)
		config.CacheSize = DefaultConfig.CacheSize
	}

	return nil
}

func (c *Config) GetLogLevel() logger.LogLevel {
	switch strings.ToLower(c.LogLevel) {
	case "debug", "trace":
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
		logger.Warningf("Unknown log_level '%s', falling back to verbosity %d", c.LogLevel, c.Verbosity)
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
			logger.Warningf("Unknown verbosity level %d, defaulting to INFO", c.Verbosity)
			return logger.INFO
		}
	}
}

func (c *Config) IsMainnet() bool {
	return c.ChainID == 1
}

func (c *Config) IsTestnet() bool {
	testnetIDs := map[uint64]bool{3: true, 4: true, 5: true, 11155111: true, 1337: true}
	return testnetIDs[c.ChainID]
}

func (c *Config) GetDataSubDir(subdir string) string {
	return filepath.Join(c.DataDir, subdir)
}
