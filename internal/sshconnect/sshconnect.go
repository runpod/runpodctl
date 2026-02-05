package sshconnect

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/runpod/runpod/internal/api"
	sshcrypto "golang.org/x/crypto/ssh"
)

const (
	defaultKeyName = "RunPod-Key-Go"
)

// KeyInfo describes the local ssh key and account match status.
type KeyInfo struct {
	Path        string `json:"path,omitempty"`
	Exists      bool   `json:"exists"`
	Source      string `json:"source,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	InAccount   *bool  `json:"in_account,omitempty"`
}

// ResolveKeyInfo returns local key info and whether it exists in the account.
// This never returns an error; missing data is simply omitted.
func ResolveKeyInfo(client *api.GraphQLClient) KeyInfo {
	keyPath, exists := defaultKeyPath()
	info := KeyInfo{
		Path:   keyPath,
		Exists: exists,
		Source: "runpodctl doctor",
	}
	if !exists {
		return info
	}

	pubFingerprint, err := readPublicKeyFingerprint(keyPath + ".pub")
	if err != nil {
		return info
	}
	info.Fingerprint = pubFingerprint

	if client == nil {
		return info
	}
	_, keys, err := client.GetPublicSSHKeys()
	if err != nil {
		return info
	}

	inAccount := false
	for _, key := range keys {
		if key.Fingerprint == pubFingerprint {
			inAccount = true
			break
		}
	}
	info.InAccount = &inAccount
	return info
}

// BuildSSHCommand builds an ssh command string using the key if available.
func BuildSSHCommand(ip string, port int, keyInfo KeyInfo) string {
	if keyInfo.Exists && keyInfo.Path != "" {
		return "ssh -i " + keyInfo.Path + " root@" + ip + " -p " + strconv.Itoa(port)
	}
	return "ssh root@" + ip + " -p " + strconv.Itoa(port)
}

// BuildConnection builds a connection map for a single pod.
func BuildConnection(pod *api.LegacyPod, keyInfo KeyInfo) map[string]interface{} {
	if pod.Runtime == nil || pod.Runtime.Ports == nil {
		return nil
	}

	for _, port := range pod.Runtime.Ports {
		if port.IsIpPublic && port.PrivatePort == 22 {
			sshCommand := BuildSSHCommand(port.Ip, port.PublicPort, keyInfo)
			conn := map[string]interface{}{
				"id":          pod.ID,
				"name":        pod.Name,
				"ssh_command": sshCommand,
				"ip":          port.Ip,
				"port":        port.PublicPort,
				"ssh_key":     keyInfo,
			}
			if !keyInfo.Exists || (keyInfo.InAccount != nil && !*keyInfo.InAccount) {
				conn["setup"] = "runpodctl doctor"
			}
			return conn
		}
	}

	return nil
}

// ListConnections builds connection maps for all pods.
func ListConnections(pods []*api.LegacyPod, keyInfo KeyInfo) []map[string]interface{} {
	var connections []map[string]interface{}
	for _, pod := range pods {
		conn := BuildConnection(pod, keyInfo)
		if conn != nil {
			connections = append(connections, conn)
		}
	}
	return connections
}

// FindPodConnection finds a pod by id or name and returns its connection.
func FindPodConnection(pods []*api.LegacyPod, nameOrID string, keyInfo KeyInfo) (*api.LegacyPod, map[string]interface{}) {
	for _, pod := range pods {
		if pod.ID == nameOrID || pod.Name == nameOrID {
			return pod, BuildConnection(pod, keyInfo)
		}
	}
	return nil, nil
}

func defaultKeyPath() (string, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	keyPath := filepath.Join(homeDir, ".runpod", "ssh", defaultKeyName)
	if _, err := os.Stat(keyPath); err == nil {
		return keyPath, true
	}
	return keyPath, false
}

func readPublicKeyFingerprint(path string) (string, error) {
	publicKey, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pubKey, _, _, _, err := sshcrypto.ParseAuthorizedKey(publicKey)
	if err != nil {
		return "", err
	}
	return sshcrypto.FingerprintSHA256(pubKey), nil
}
