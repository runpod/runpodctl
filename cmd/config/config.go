package config

import (
	"cli/api"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

var ConfigFile string
var apiKey string
var apiUrl string

var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "CLI Config",
	Long:  "RunPod CLI Config Settings",
	Run: func(c *cobra.Command, args []string) {
		err := viper.WriteConfig()
		cobra.CheckErr(err)
		fmt.Println("saved apiKey into config file: " + ConfigFile)
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Println("couldn't get user home dir path")
			return
		}
		sshFolderPath := filepath.Join(home, ".runpod", "ssh")
		os.MkdirAll(sshFolderPath, os.ModePerm)
		privateSshPath := filepath.Join(sshFolderPath, "RunPod-Key-Go")
		publicSshPath := filepath.Join(sshFolderPath, "RunPod-Key-Go.pub")
		publicKey, _ := os.ReadFile(publicSshPath)
		if _, err := os.Stat(privateSshPath); errors.Is(err, os.ErrNotExist) {
			publicKey = makeRSAKey(privateSshPath)
		}
		api.AddPublicSSHKey(publicKey)
	},
}

func init() {
	ConfigCmd.Flags().StringVar(&apiKey, "apiKey", "", "runpod api key")
	ConfigCmd.MarkFlagRequired("apiKey")
	viper.BindPFlag("apiKey", ConfigCmd.Flags().Lookup("apiKey")) //nolint
	viper.SetDefault("apiKey", "")

	ConfigCmd.Flags().StringVar(&apiUrl, "apiUrl", "", "runpod api url")
	viper.BindPFlag("apiUrl", ConfigCmd.Flags().Lookup("apiUrl")) //nolint
	viper.SetDefault("apiUrl", "https://api.runpod.io/graphql")
}

func makeRSAKey(filename string) []byte {
	bitSize := 2048

	// Generate RSA key.
	key, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		panic(err)
	}

	// Extract public component.
	pub := key.PublicKey

	// Encode private key to PKCS#1 ASN.1 PEM.
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)

	// generate and write public key
	publicKey, err := ssh.NewPublicKey(&pub)
	if err != nil {
		fmt.Println("err in NewPublicKey")
		fmt.Println(err)
	}
	pubBytes := ssh.MarshalAuthorizedKey(publicKey)
	pubBytes = append(pubBytes, []byte(" "+filename)...)

	// Write private key to file.
	if err := os.WriteFile(filename, keyPEM, 0600); err != nil {
		fmt.Println("err writing priv")
		panic(err)
	}

	// Write public key to file.
	if err := os.WriteFile(filename+".pub", pubBytes, 0600); err != nil {
		fmt.Println("err writing pub")
		panic(err)
	}
	fmt.Println("saved new SSH public key into", filename+".pub")
	return pubBytes
}
