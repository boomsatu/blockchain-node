package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings" // Diperlukan untuk SetEnvKeyReplacer

	"blockchain-node/config" // Impor package config Anda

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd merepresentasikan perintah dasar ketika dipanggil tanpa sub-perintah
var rootCmd = &cobra.Command{
	Use:   "lumina", // Ganti dengan nama executable Anda jika berbeda
	Short: "Lumina Blockchain Node",
	Long: `Lumina Blockchain adalah implementasi node blockchain kustom
yang mendukung mining, transaksi, dan RPC API.`,
	// SilenceUsage: true, // Bisa ditambahkan untuk tidak menampilkan usage pada error RunE
	// SilenceErrors: true, // Jika Anda menangani semua error secara manual dan tidak ingin Cobra mencetaknya
}

// Execute menambahkan semua perintah anak ke perintah root dan mengatur flag dengan sesuai.
// Ini dipanggil oleh main.main(). Ini hanya perlu terjadi sekali pada rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Fungsi initConfig akan dipanggil ketika Cobra menginisialisasi.
	cobra.OnInitialize(initConfig)

	// Menambahkan perintah 'startnode' ke perintah root.
	// Pastikan startNodeCmd didefinisikan di cmd/startnode.go
	rootCmd.AddCommand(startNodeCmd)

	// Menambahkan perintah terkait wallet
	rootCmd.AddCommand(createwalletCmd) // Dari cmd/wallet.go
	rootCmd.AddCommand(getbalanceCmd)   // Dari cmd/wallet.go
	rootCmd.AddCommand(sendCmd)         // Dari cmd/wallet.go

	// Mendefinisikan flag persisten yang berlaku untuk perintah ini dan semua sub-perintahnya.
	// Nilai default di sini akan digunakan jika tidak ada nilai dari file config atau ENV.
	// Namun, viper.Unmarshal nanti akan menimpa ini dengan nilai dari config.defaultConfig jika tidak ada sumber lain.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.lumina/config.yaml or ./config.yaml)")

	// Flag-flag ini juga didefinisikan di config.defaultConfig. Viper akan menangani prioritas.
	// Default yang diberikan di sini lebih sebagai placeholder untuk help text.
	rootCmd.PersistentFlags().String("datadir", config.DefaultConfig.DataDir, "Data directory for blockchain data")
	rootCmd.PersistentFlags().Int("port", config.DefaultConfig.Port, "P2P port")
	rootCmd.PersistentFlags().Int("rpcport", config.DefaultConfig.RPCPort, "JSON-RPC port")
	rootCmd.PersistentFlags().String("rpcaddr", config.DefaultConfig.RPCAddr, "JSON-RPC address (0.0.0.0 to listen on all interfaces)")
	rootCmd.PersistentFlags().Uint64("chainid", config.DefaultConfig.ChainID, "Chain ID for the blockchain network")
	rootCmd.PersistentFlags().String("log_level", config.DefaultConfig.LogLevel, "Logging level (debug, info, warn, error, fatal)")
	rootCmd.PersistentFlags().Bool("enable_p2p", config.DefaultConfig.EnableP2P, "Enable P2P networking")

	// Flag baru untuk kontrol genesis (BARU)
	rootCmd.PersistentFlags().Bool("forcegenesis", config.DefaultConfig.ForceGenesis, "Force creation of a new genesis block from genesis.json, potentially overwriting existing data in datadir.")
	rootCmd.PersistentFlags().String("genesisfilepath", config.DefaultConfig.GenesisFilePath, "Path to the genesis.json file.")

	// Bind flag persisten ke Viper. Ini memungkinkan nilai flag CLI menimpa nilai dari file config/env.
	viper.BindPFlag("datadir", rootCmd.PersistentFlags().Lookup("datadir"))
	viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("rpcport", rootCmd.PersistentFlags().Lookup("rpcport"))
	viper.BindPFlag("rpcaddr", rootCmd.PersistentFlags().Lookup("rpcaddr"))
	viper.BindPFlag("chainid", rootCmd.PersistentFlags().Lookup("chainid"))
	viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log_level"))
	viper.BindPFlag("enable_p2p", rootCmd.PersistentFlags().Lookup("enable_p2p"))

	// Ikat flag genesis baru ke Viper (BARU)
	viper.BindPFlag("forcegenesis", rootCmd.PersistentFlags().Lookup("forcegenesis"))
	viper.BindPFlag("genesisfilepath", rootCmd.PersistentFlags().Lookup("genesisfilepath"))

	// Flag yang spesifik untuk sub-perintah (seperti --mining untuk startnode)
	// sebaiknya didefinisikan di file sub-perintah tersebut (misalnya, cmd/startnode.go)
	// dan di-bind ke Viper di sana jika perlu.
	// Contoh:
	// startNodeCmd.Flags().Bool("mining", config.DefaultConfig.Mining, "Enable mining")
	// viper.BindPFlag("mining", startNodeCmd.Flags().Lookup("mining"))
	// startNodeCmd.Flags().String("miner", config.DefaultConfig.Miner, "Miner address for rewards")
	// viper.BindPFlag("miner", startNodeCmd.Flags().Lookup("miner"))
}

// initConfig membaca file konfigurasi dan variabel ENV jika ada.
// Fungsi ini dipanggil oleh cobra.OnInitialize.
func initConfig() {
	if cfgFile != "" {
		// Gunakan file konfigurasi dari flag jika disediakan.
		viper.SetConfigFile(cfgFile)
	} else {
		// Cari file konfigurasi di direktori home dan direktori kerja.
		home, err := os.UserHomeDir()
		if err == nil { // Jangan error jika tidak bisa mendapatkan home dir, cukup jangan tambahkan path itu.
			viper.AddConfigPath(filepath.Join(home, ".lumina")) // Contoh: $HOME/.lumina/config.yaml
		}
		viper.AddConfigPath(".")        // Direktori kerja saat ini.
		viper.AddConfigPath("./config") // Direktori config/ di dalam direktori kerja.
		viper.SetConfigName("config")   // Nama file konfigurasi (tanpa ekstensi).
		viper.SetConfigType("yaml")     // Tipe file konfigurasi.
	}

	// Baca juga variabel lingkungan yang cocok.
	viper.AutomaticEnv()                                             // Baca variabel lingkungan.
	viper.SetEnvPrefix("LUMINA")                                     // Prefix untuk variabel lingkungan (misalnya, LUMINA_DATADIR).
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_")) // Ganti . dan - dengan _ di nama ENV var.

	// Jika file konfigurasi ditemukan, baca isinya.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	} else {
		// Jika error BUKAN karena file tidak ditemukan, laporkan.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config file '%s': %s\n", viper.ConfigFileUsed(), err)
		}
		// Jika file tidak ditemukan, itu tidak selalu error, karena kita punya default dan ENV.
		// Pesan warning bisa ditambahkan di sini jika diinginkan.
		// fmt.Fprintln(os.Stderr, "Warning: Config file not found. Using defaults, environment variables, and flags.")
	}
}
