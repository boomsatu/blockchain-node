package cmd

import (
	"blockchain-node/wallet"
	"fmt"

	"github.com/spf13/cobra"
)

var createwalletCmd = &cobra.Command{
	Use:   "createwallet",
	Short: "Create a new wallet",
	Long:  `Create a new wallet with private/public key pair`,
	Run: func(cmd *cobra.Command, args []string) {
		createWallet()
	},
}

var getbalanceCmd = &cobra.Command{
	Use:   "getbalance [address]",
	Short: "Get balance of an address",
	Long:  `Get the balance of a specific address`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		getBalance(args[0])
	},
}

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send transaction",
	Long:  `Send a transaction from one address to another`,
	Run: func(cmd *cobra.Command, args []string) {
		from, _ := cmd.Flags().GetString("from")
		to, _ := cmd.Flags().GetString("to")
		amount, _ := cmd.Flags().GetString("amount")
		data, _ := cmd.Flags().GetString("data")
		gasLimit, _ := cmd.Flags().GetUint64("gaslimit")
		gasPrice, _ := cmd.Flags().GetString("gasprice")

		sendTransaction(from, to, amount, data, gasLimit, gasPrice)
	},
}

func init() {
	rootCmd.AddCommand(createwalletCmd)
	rootCmd.AddCommand(getbalanceCmd)
	rootCmd.AddCommand(sendCmd)

	sendCmd.Flags().StringP("from", "f", "", "From address")
	sendCmd.Flags().StringP("to", "t", "", "To address")
	sendCmd.Flags().StringP("amount", "a", "0", "Amount to send")
	sendCmd.Flags().StringP("data", "d", "", "Transaction data (hex)")
	sendCmd.Flags().Uint64P("gaslimit", "g", 21000, "Gas limit")
	sendCmd.Flags().StringP("gasprice", "p", "20000000000", "Gas price in wei")

	sendCmd.MarkFlagRequired("from")
	sendCmd.MarkFlagRequired("to")
}

func createWallet() {
	w, err := wallet.NewWallet()
	if err != nil {
		fmt.Printf("Failed to create wallet: %v\n", err)
		return
	}

	fmt.Printf("New wallet created!\n")
	fmt.Printf("Address: %s\n", "0x"+w.GetAddress())
	fmt.Printf("Private Key: %s\n", "0x"+w.GetPrivateKeyHex())
	fmt.Printf("Public Key: %s\n", w.GetPublicKeyHex())
}

func getBalance(address string) {
	// This would connect to a running node via RPC
	fmt.Printf("Getting balance for address: %s\n", address)
	fmt.Printf("Balance: 0 ETH (connect to running node for actual balance)\n")
}

func sendTransaction(from, to, amount, data string, gasLimit uint64, gasPrice string) {
	fmt.Printf("Sending transaction:\n")
	fmt.Printf("From: %s\n", from)
	fmt.Printf("To: %s\n", to)
	fmt.Printf("Amount: %s\n", amount)
	fmt.Printf("Data: %s\n", data)
	fmt.Printf("Gas Limit: %d\n", gasLimit)
	fmt.Printf("Gas Price: %s\n", gasPrice)
	fmt.Printf("Transaction would be sent to running node via RPC\n")
}
