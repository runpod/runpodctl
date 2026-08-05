package sshconnect

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/podstate"
	sshcrypto "golang.org/x/crypto/ssh"
)

const (
	defaultKeyName = "runpodctl-ssh-key"
	legacyKeyName  = "RunPod-Key-Go"
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

// PublicSSHPort returns the pod's publicly reachable ssh address (private port
// 22 mapped to a public port). ok is false while the pod has no runtime yet or
// the port has not been allocated.
//
// Allocation is not readiness: the port can be listed minutes before sshd
// accepts a connection (or forever, if the image runs no sshd), so callers that
// need "ssh works" must probe the address as well.
func PublicSSHPort(pod *api.LegacyPod) (ip string, port int, ok bool) {
	if pod == nil || pod.Runtime == nil || pod.Runtime.Ports == nil {
		return "", 0, false
	}
	for _, p := range pod.Runtime.Ports {
		if p.IsIpPublic && p.PrivatePort == 22 {
			return p.Ip, p.PublicPort, true
		}
	}
	return "", 0, false
}

// BuildConnection builds a connection map for a single pod.
func BuildConnection(pod *api.LegacyPod, keyInfo KeyInfo) map[string]interface{} {
	ip, port, ok := PublicSSHPort(pod)
	if !ok {
		return nil
	}

	conn := map[string]interface{}{
		"id":          pod.ID,
		"name":        pod.Name,
		"ssh_command": BuildSSHCommand(ip, port, keyInfo),
		"ip":          ip,
		"port":        port,
		"ssh_key":     keyInfo,
	}
	if !keyInfo.Exists || (keyInfo.InAccount != nil && !*keyInfo.InAccount) {
		conn["setup"] = "runpodctl doctor"
	}
	return conn
}

// ListConnections builds connection maps for all pods that can actually be
// reached.
//
// A stopped or terminated pod keeps reporting its old runtime ports for a while,
// so BuildConnection will happily produce an ssh command for a container that no
// longer exists. Those pods are skipped rather than listed, since the whole
// point of this output is "here is what you can ssh into".
func ListConnections(pods []*api.LegacyPod, keyInfo KeyInfo) []map[string]interface{} {
	// non-nil so "nothing reachable" serialises as [] rather than null: this
	// filters more pods than it used to, so the empty case is now common.
	connections := make([]map[string]interface{}, 0, len(pods))
	for _, pod := range pods {
		// nil: graphql lists are nullable, and BuildConnection would dereference
		// it. known-down: a stopped pod keeps reporting stale runtime ports, so it
		// would be listed with a command that cannot connect.
		if pod == nil || PodState(pod).IsKnownDown() {
			continue
		}
		conn := BuildConnection(pod, keyInfo)
		if conn != nil {
			connections = append(connections, conn)
		}
	}
	return connections
}

// PodState derives the runtime state of a graphql pod. It lives here so every
// ssh path uses one derivation with one set of signals, instead of each caller
// assembling podstate.Signals slightly differently.
func PodState(pod *api.LegacyPod) podstate.State {
	if pod == nil {
		return podstate.State{Status: podstate.StatusUnknown, Reason: podstate.ReasonRuntimeUnavailable}
	}
	return podstate.Derive(podstate.Signals{
		DesiredStatus:    pod.DesiredStatus,
		LastStatusChange: pod.LastStatusChange,
		RuntimeProbed:    true,
		RuntimeReported:  pod.Runtime != nil,
	})
}

// FindPodConnection finds a pod by id or name and returns its connection.
//
// A nil entry in the list is skipped rather than dereferenced: graphql lists are
// nullable, and `pod create --wait` re-reads this list right after telling the
// user the pod is ready, where a panic would replace the single json error object
// with a go stack trace on stderr and exit 2.
func FindPodConnection(pods []*api.LegacyPod, nameOrID string, keyInfo KeyInfo) (*api.LegacyPod, map[string]interface{}) {
	for _, pod := range pods {
		if pod == nil {
			continue
		}
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
	sshDir := filepath.Join(homeDir, ".runpod", "ssh")

	// check new name first, then fall back to legacy name
	newPath := filepath.Join(sshDir, defaultKeyName)
	if _, err := os.Stat(newPath); err == nil {
		return newPath, true
	}
	legacyPath := filepath.Join(sshDir, legacyKeyName)
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, true
	}
	return newPath, false
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
