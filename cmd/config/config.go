package config

import (
	"fmt"
	"strings"

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
	RunE: func(c *cobra.Command, args []string) error {
		if err := saveConfig(); err != nil {
			return fmt.Errorf("error saving config: %w", err)
		}

		publicKey, err := getOrCreateSSHKey()
		if err != nil {
			return fmt.Errorf("failed to get or create local SSH key: %w", err)
		}

		if err := updateSSHKeyInCloud(publicKey); err != nil {
			return fmt.Errorf("failed to update SSH key in the cloud: %w", err)
		}

		return nil
	},
}

// saveConfig saves the CLI configuration to a file
func saveConfig() error {
	if err := viper.WriteConfig(); err != nil {
		return err
	}
	fmt.Println("Configuration saved to file:", viper.ConfigFileUsed())
	return nil
}

// Checks for an existing local SSH key and generates a new one if not found
func getOrCreateSSHKey() ([]byte, error) {
	publicKey, err := ssh.GetLocalSSHKey()
	if err != nil {
		return nil, fmt.Errorf("error checking for local SSH key: %w", err)
	}

	if publicKey == nil {
		fmt.Println("No existing local SSH key found, generating a new one.")
		publicKey, err = ssh.GenerateSSHKeyPair("RunPod-Key-Go")
		if err != nil {
			return nil, fmt.Errorf("failed to generate SSH key: %w", err)
		}
		fmt.Println("New SSH key pair generated.")
	} else {
		fmt.Println("Existing local SSH key found.")
	}

	return publicKey, nil
}

// Checks if the SSH key exists in the cloud and adds it if necessary
func updateSSHKeyInCloud(publicKey []byte) error {
	_, cloudKeys, err := api.GetPublicSSHKeys()
	if err != nil {
		return fmt.Errorf("failed to get SSH key in the cloud: %w", err)
	}

	// Trim any whitespace from the publicKey
	publicKeyStr := strings.TrimSpace(string(publicKey))

	// Check if the publicKey already exists in the cloud
	for _, cloudKey := range cloudKeys {
		if strings.TrimSpace(cloudKey.Key) == publicKeyStr {
			fmt.Println("SSH key already exists in the cloud. No action needed.")
			return nil
		}
	}

	if err := api.AddPublicSSHKey(publicKey); err != nil {
		return fmt.Errorf("failed to add the SSH key: %w", err)
	}

	fmt.Println("SSH key added successfully to the cloud.")
	return nil
}

func init() {
	ConfigCmd.Flags().StringVar(&apiKey, "apiKey", "", "RunPod API key")
	viper.BindPFlag("apiKey", ConfigCmd.Flags().Lookup("apiKey")) //nolint
	viper.SetDefault("apiKey", "")

	ConfigCmd.Flags().StringVar(&apiUrl, "apiUrl", "https://api.runpod.io/graphql", "RunPod API URL")
	viper.BindPFlag("apiUrl", ConfigCmd.Flags().Lookup("apiUrl")) //nolint

	ConfigCmd.MarkFlagRequired("apiKey")
}
