package config

import (
	"fmt"
	"os"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/cmd/ssh"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	ConfigFile string
	apiKey     string
	apiUrl     string
)

var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  "RunPod CLI Config Settings",
	Run: func(c *cobra.Command, args []string) {
		if err := viper.WriteConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			return
		}
		fmt.Println("Configuration saved to file:", viper.ConfigFileUsed())

		publicKey, err := ssh.GenerateSSHKeyPair("RunPod-Key-Go")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to generate SSH key: %v\n", err)
			return
		}

		if err := api.AddPublicSSHKey(publicKey); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to add the SSH key: %v\n", err)
			return
		}
		fmt.Println("SSH key added successfully.")
	},
}

func init() {
	ConfigCmd.Flags().StringVar(&apiKey, "apiKey", "", "RunPod API key")
	viper.BindPFlag("apiKey", ConfigCmd.Flags().Lookup("apiKey")) //nolint
	viper.SetDefault("apiKey", "")

	ConfigCmd.Flags().StringVar(&apiUrl, "apiUrl", "https://api.runpod.io/graphql", "RunPod API URL")
	viper.BindPFlag("apiUrl", ConfigCmd.Flags().Lookup("apiUrl")) //nolint

	ConfigCmd.MarkFlagRequired("apiKey")
}
