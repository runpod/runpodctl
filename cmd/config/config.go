package config

import (
	"fmt"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/cmd/ssh"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	sshcrypto "golang.org/x/crypto/ssh"
)

var (
	ConfigFile string
	apiKey     string
	apiUrl     string
)

var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  "Runpod CLI Config Settings",
	RunE: func(c *cobra.Command, args []string) error {
		if err := saveConfig(); err != nil {
			return fmt.Errorf("error saving config: %w", err)
		}

		publicKey, err := getOrCreateSSHKey()
		if err != nil {
			return fmt.Errorf("failed to get or create local SSH key: %w", err)
		}

		if err := ensureSSHKeyInCloud(publicKey); err != nil {
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
		publicKey, err = ssh.GenerateSSHKeyPair("Runpod-Key-Go")
		if err != nil {
			return nil, fmt.Errorf("failed to generate SSH key: %w", err)
		}
		fmt.Println("New SSH key pair generated.")
	} else {
		fmt.Println("Existing local SSH key found.")
	}

	return publicKey, nil
}

// ensureSSHKeyInCloud checks if the SSH key exists in the cloud and adds it if necessary
func ensureSSHKeyInCloud(publicKey []byte) error {
	_, cloudKeys, err := api.GetPublicSSHKeys()
	if err != nil {
		return fmt.Errorf("failed to get SSH keys from the cloud: %w", err)
	}

	// Parse the local public key
	localPubKey, _, _, _, err := sshcrypto.ParseAuthorizedKey(publicKey)
	if err != nil {
		return fmt.Errorf("failed to parse local public key: %w", err)
	}

	localFingerprint := sshcrypto.FingerprintSHA256(localPubKey)

	// Check if the publicKey already exists in the cloud
	for _, cloudKey := range cloudKeys {
		if cloudKey.Fingerprint == localFingerprint {
			fmt.Println("SSH key already exists in the cloud. No action needed.")
			return nil
		}
	}

	// If the key doesn't exist, add it
	if err := api.AddPublicSSHKey(publicKey); err != nil {
		return fmt.Errorf("failed to add the SSH key: %w", err)
	}

	fmt.Println("SSH key added successfully to the cloud.")
	return nil
}

func init() {
	ConfigCmd.Flags().StringVar(&apiKey, "apiKey", "", "Runpod API key")
	viper.BindPFlag("apiKey", ConfigCmd.Flags().Lookup("apiKey")) //nolint
	viper.SetDefault("apiKey", "")

	ConfigCmd.Flags().StringVar(&apiUrl, "apiUrl", "https://api.runpod.io/graphql", "Runpod API URL")
	viper.BindPFlag("apiUrl", ConfigCmd.Flags().Lookup("apiUrl")) //nolint

	ConfigCmd.MarkFlagRequired("apiKey")
}
