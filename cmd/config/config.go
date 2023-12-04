package config

import (
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
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Println("couldn't get user home dir path")
			return
		}
		sshPath := filepath.Join(home, ".runpod", "ssh", "RunPod-Key-Go")
		if _, err := os.Stat(sshPath); errors.Is(err, os.ErrNotExist) {
			makeRSAKey(sshPath)
		}
		fmt.Println("saved apiKey into config file: " + ConfigFile)
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

func makeRSAKey(filename string) {
	bitSize := 2048

	// Generate RSA key.
	key, err := rsa.GenerateKey(rand.Reader, bitSize)
	if err != nil {
		panic(err)
	}

	// Extract public component.
	pub := key.Public()

	// Encode private key to PKCS#1 ASN.1 PEM.
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)

	// Encode public key to PKCS#1 ASN.1 PEM.
	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: x509.MarshalPKCS1PublicKey(pub.(*rsa.PublicKey)),
		},
	)

	// Write private key to file.
	if err := os.WriteFile(filename, keyPEM, 0600); err != nil {
		panic(err)
	}

	// Write public key to file.
	if err := os.WriteFile(filename+".pub", pubPEM, 0600); err != nil {
		panic(err)
	}
	fmt.Println("saved new SSH public key into", filename+".pub")
}
