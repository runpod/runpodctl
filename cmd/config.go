package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpod/internal/api"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

var (
	apiKey string
	apiURL string
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "configure cli settings",
	Long:  "configure runpod cli settings including api key",
	RunE:  runConfig,
}

func init() {
	configCmd.Flags().StringVar(&apiKey, "apiKey", "", "runpod api key")
	viper.BindPFlag("apiKey", configCmd.Flags().Lookup("apiKey")) //nolint:errcheck
	viper.SetDefault("apiKey", "")

	configCmd.Flags().StringVar(&apiURL, "apiUrl", "https://api.runpod.io/graphql", "runpod graphql api url")
	viper.BindPFlag("apiUrl", configCmd.Flags().Lookup("apiUrl")) //nolint:errcheck
}

func runConfig(cmd *cobra.Command, args []string) error {
	if err := saveConfig(); err != nil {
		return fmt.Errorf("error saving config: %w", err)
	}

	publicKey, err := getOrCreateSSHKey()
	if err != nil {
		return fmt.Errorf("failed to get or create local ssh key: %w", err)
	}

	if err := ensureSSHKeyInCloud(publicKey); err != nil {
		return fmt.Errorf("failed to update ssh key in the cloud: %w", err)
	}

	return nil
}

func saveConfig() error {
	if err := viper.WriteConfig(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "configuration saved to:", viper.ConfigFileUsed())
	return nil
}

func getOrCreateSSHKey() ([]byte, error) {
	publicKey, err := getLocalSSHKey()
	if err != nil {
		return nil, fmt.Errorf("error checking for local ssh key: %w", err)
	}

	if publicKey == nil {
		fmt.Fprintln(os.Stderr, "no existing local ssh key found, generating a new one")
		publicKey, err = generateSSHKeyPair("runpod-cli-key")
		if err != nil {
			return nil, fmt.Errorf("failed to generate ssh key: %w", err)
		}
		fmt.Fprintln(os.Stderr, "new ssh key pair generated")
	} else {
		fmt.Fprintln(os.Stderr, "existing local ssh key found")
	}

	return publicKey, nil
}

func ensureSSHKeyInCloud(publicKey []byte) error {
	client, err := api.NewGraphQLClient()
	if err != nil {
		return err
	}

	_, cloudKeys, err := client.GetPublicSSHKeys()
	if err != nil {
		return fmt.Errorf("failed to get ssh keys from the cloud: %w", err)
	}

	localPubKey, _, _, _, err := ssh.ParseAuthorizedKey(publicKey)
	if err != nil {
		return fmt.Errorf("failed to parse local public key: %w", err)
	}

	localFingerprint := ssh.FingerprintSHA256(localPubKey)

	for _, cloudKey := range cloudKeys {
		if cloudKey.Fingerprint == localFingerprint {
			fmt.Fprintln(os.Stderr, "ssh key already exists in the cloud")
			return nil
		}
	}

	if err := client.AddPublicSSHKey(publicKey); err != nil {
		return fmt.Errorf("failed to add the ssh key: %w", err)
	}

	fmt.Fprintln(os.Stderr, "ssh key added to the cloud")
	return nil
}

func getLocalSSHKey() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	keyPath := home + "/.runpod/ssh/runpod-cli-key.pub"
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return nil, nil
	}

	return os.ReadFile(keyPath)
}

func generateSSHKeyPair(keyName string) ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	sshDir := home + "/.runpod/ssh"
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, err
	}

	keyPath := sshDir + "/" + keyName

	// Generate ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Convert to SSH format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to convert public key: %w", err)
	}

	// Format public key with comment
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPubKey)
	pubKeyStr := strings.TrimSpace(string(pubKeyBytes)) + " " + strings.ReplaceAll(keyName, " ", "-") + "\n"

	// Write public key
	if err := os.WriteFile(keyPath+".pub", []byte(pubKeyStr), 0644); err != nil {
		return nil, fmt.Errorf("failed to write public key: %w", err)
	}

	// Write private key in OpenSSH format
	privKeyPEM := &pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: marshalED25519PrivateKey(privKey),
	}
	privKeyBytes := pem.EncodeToMemory(privKeyPEM)
	if err := os.WriteFile(keyPath, privKeyBytes, 0600); err != nil {
		return nil, fmt.Errorf("failed to write private key: %w", err)
	}

	return []byte(pubKeyStr), nil
}

// marshalED25519PrivateKey creates an OpenSSH format private key
func marshalED25519PrivateKey(key ed25519.PrivateKey) []byte {
	// This is a simplified version - for production use golang.org/x/crypto/ssh
	// The actual OpenSSH format is more complex
	pubKey := key.Public().(ed25519.PublicKey)

	// Build the OpenSSH private key format
	var w struct {
		CipherName   string
		KdfName      string
		KdfOpts      string
		NumKeys      uint32
		PubKey       []byte
		PrivKeyBlock []byte
	}

	w.CipherName = "none"
	w.KdfName = "none"
	w.KdfOpts = ""
	w.NumKeys = 1

	sshPubKey, _ := ssh.NewPublicKey(pubKey)
	w.PubKey = sshPubKey.Marshal()

	pk := struct {
		Check1  uint32
		Check2  uint32
		Keytype string
		Pub     []byte
		Priv    []byte
		Comment string
		Pad     []byte `ssh:"rest"`
	}{}

	pk.Check1 = 0x12345678
	pk.Check2 = 0x12345678
	pk.Keytype = ssh.KeyAlgoED25519
	pk.Pub = pubKey
	pk.Priv = key
	pk.Comment = ""

	// Add padding
	block := ssh.Marshal(pk)
	padLen := (8 - len(block)%8) % 8
	for i := 0; i < padLen; i++ {
		block = append(block, byte(i+1))
	}
	w.PrivKeyBlock = block

	magic := []byte("openssh-key-v1\x00")
	result := append(magic, ssh.Marshal(w)...)
	return result
}
