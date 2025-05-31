package cmd

import (
	"fmt"
	"os"
	"path/filepath" // Ditambahkan untuk path home

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// rootCmd merepresentasikan perintah dasar ketika dipanggil tanpa sub-perintah
var rootCmd = &cobra.Command{
	Use:   "lumina", // Mengubah Use agar sesuai dengan nama executable Anda
	Short: "Lumina Blockchain Node",
	Long: `Lumina Blockchain adalah implementasi node blockchain kustom
yang mendukung mining, transaksi, dan RPC API.`,
}

// Execute menambahkan semua perintah anak ke perintah root dan mengatur flag dengan sesuai.
// Ini dipanggil oleh main.main(). Ini hanya perlu terjadi sekali pada rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Menambahkan startNodeCmd (dan perintah lain jika ada) ke rootCmd
	// Pastikan startNodeCmd didefinisikan di cmd/startnode.go sebagai variabel package-level
	// dan package cmd/startnode.go diimpor (biasanya otomatis jika dalam package yang sama
	// atau jika Anda memanggil fungsi dari package tersebut yang mendaftarkannya).
	// Cara paling umum adalah mendaftarkannya di sini:
	rootCmd.AddCommand(startNodeCmd) // Baris ini penting
	// Jika ada perintah lain seperti walletCmd, tambahkan juga:
	// rootCmd.AddCommand(walletCmd) // Contoh

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.lumina/config.yaml or ./config.yaml)")
	rootCmd.PersistentFlags().String("datadir", "./data_node_default", "Data directory for blockchain data") // Default diubah agar lebih jelas
	rootCmd.PersistentFlags().Int("port", 30303, "P2P port")
	rootCmd.PersistentFlags().Int("rpcport", 8545, "JSON-RPC port")
	rootCmd.PersistentFlags().String("rpcaddr", "0.0.0.0", "JSON-RPC address (0.0.0.0 to listen on all interfaces)")

	// Bind flag ke Viper agar nilai dari flag bisa menimpa nilai dari file config/env
	// dan agar nilai default flag bisa digunakan jika tidak ada di config/env.
	viper.BindPFlag("datadir", rootCmd.PersistentFlags().Lookup("datadir"))
	viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("rpcport", rootCmd.PersistentFlags().Lookup("rpcport"))
	viper.BindPFlag("rpcaddr", rootCmd.PersistentFlags().Lookup("rpcaddr"))
}

// initConfig membaca file konfigurasi dan variabel ENV jika ada.
// cmd/root.go
func initConfig() {
	if cfgFile != "" { // cfgFile diisi oleh flag --config seperti "--config config2.yaml"
		viper.SetConfigFile(cfgFile)
	} else {
		// Logika pencarian file default jika --config tidak digunakan
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(filepath.Join(home, ".lumina")) // Contoh: $HOME/.lumina/config.yaml
		}
		viper.AddConfigPath(".")      // Direktori kerja saat ini
		viper.SetConfigName("config") // Nama file default (misalnya config.yaml)
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("LUMINA") // Atau prefix lain yang Anda inginkan

	// Coba baca file konfigurasi. Ini adalah langkah penting di sini.
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// File tidak ditemukan, akan menggunakan default + env + flags. Ini tidak selalu error.
			fmt.Fprintln(os.Stderr, "Warning: Config file not found. Using defaults, environment variables, and flags.")
		} else {
			// Error lain saat membaca file konfigurasi.
			fmt.Fprintf(os.Stderr, "Error reading config file (%s): %s\n", viper.ConfigFileUsed(), err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
