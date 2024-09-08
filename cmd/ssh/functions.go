package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// GenerateSSHKeyPair generates an RSA key pair and saves the private key to a file in the user's home directory.
func GenerateSSHKeyPair(keyName string) ([]byte, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	sshFolderPath := filepath.Join(homeDir, ".runpod", "ssh")

	// Ensure the SSH directory exists
	if err := os.MkdirAll(sshFolderPath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create SSH directory: %w", err)
	}

	privateKeyPath := filepath.Join(sshFolderPath, keyName)
	publicKeyPath := privateKeyPath + ".pub"

	// Generate RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Encode private key to PEM and write to file
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("writing private key file: %w", err)
	}

	// Generate SSH public key, append the key name as a comment, and write to file
	publicKeySSH, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SSH public key: %w", err)
	}
	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKeySSH)

	// Remove newlines from the end of the public key
	publicKeyBytes = []byte(strings.TrimSuffix(string(publicKeyBytes), "\n"))
	publicKeyBytes = append(publicKeyBytes, []byte(" "+keyName+"\n")...)

	if err := os.WriteFile(publicKeyPath, publicKeyBytes, 0644); err != nil {
		return nil, fmt.Errorf("failed to write public key file: %w", err)
	}

	fmt.Printf("SSH key pair generated: %s (private), %s (public)\n", privateKeyPath, publicKeyPath)
	return publicKeyBytes, nil
}

func GetLocalSSHKey() ([]byte, error) {
	usr, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	keyPath := filepath.Join(usr, ".ssh", "RunPod-Key-Go.pub")

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return nil, nil // No existing key found
	}

	publicKey, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read existing public key: %w", err)
	}

	return publicKey, nil
}
