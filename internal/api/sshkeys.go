package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSH keys live on the account: rest v2 lists them with GET /account/ssh-keys
// and replaces the whole set with PUT. Add and remove are read-modify-write.

type sshKeysBody struct {
	Keys []string `json:"keys"`
}

func (c *Client) getSSHKeyLines() ([]string, error) {
	data, err := c.GetV2("/account/ssh-keys", nil)
	if err != nil {
		return nil, err
	}
	var body sshKeysBody
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return body.Keys, nil
}

func (c *Client) putSSHKeyLines(lines []string) error {
	if lines == nil {
		lines = []string{}
	}
	_, err := c.PutV2("/account/ssh-keys", &sshKeysBody{Keys: lines})
	return err
}

func parseSSHKey(line string) (SSHKey, bool) {
	pubKey, name, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return SSHKey{}, false
	}
	return SSHKey{
		Name:        name,
		Type:        pubKey.Type(),
		Key:         string(ssh.MarshalAuthorizedKey(pubKey)),
		Fingerprint: ssh.FingerprintSHA256(pubKey),
	}, true
}

// GetPublicSSHKeys returns the account's keys. lines that do not parse as a
// public key are left out, as before.
func (c *Client) GetPublicSSHKeys() ([]SSHKey, error) {
	lines, err := c.getSSHKeyLines()
	if err != nil {
		return nil, err
	}
	var keys []SSHKey
	for _, line := range lines {
		if key, ok := parseSSHKey(line); ok {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// AddPublicSSHKey adds each key in key (one per line) that the account does
// not already have. keys are compared by fingerprint, so a comment does not
// make a registered key look new. v2 stores one key per entry, so a pasted
// block of several keys is split.
func (c *Client) AddPublicSSHKey(key []byte) error {
	lines, err := c.getSSHKeyLines()
	if err != nil {
		return fmt.Errorf("failed to get existing SSH keys: %w", err)
	}
	have := map[string]bool{}
	for _, line := range lines {
		if existing, ok := parseSSHKey(line); ok {
			have[existing.Fingerprint] = true
		}
	}
	added := false
	for _, newKey := range strings.Split(string(key), "\n") {
		newKey = strings.TrimSpace(newKey)
		if newKey == "" {
			continue
		}
		if parsed, ok := parseSSHKey(newKey); ok {
			if have[parsed.Fingerprint] {
				continue
			}
			have[parsed.Fingerprint] = true
		}
		lines = append(lines, newKey)
		added = true
	}
	if !added {
		return nil
	}
	if err := c.putSSHKeyLines(lines); err != nil {
		return fmt.Errorf("failed to update SSH keys: %w", err)
	}
	return nil
}

// RemovePublicSSHKey removes the key matching name and/or fingerprint. a name
// matching several keys is ambiguous and needs the fingerprint.
func (c *Client) RemovePublicSSHKey(name, fingerprint string) error {
	lines, err := c.getSSHKeyLines()
	if err != nil {
		return fmt.Errorf("failed to get existing SSH keys: %w", err)
	}

	var kept []string
	matchCount := 0
	for _, line := range lines {
		if key, ok := parseSSHKey(line); ok && sshKeyMatches(key, name, fingerprint) {
			matchCount++
			continue
		}
		kept = append(kept, line)
	}

	switch {
	case matchCount == 0:
		return fmt.Errorf("ssh key not found")
	case name != "" && fingerprint == "" && matchCount > 1:
		return fmt.Errorf("multiple ssh keys found with name %q; use --fingerprint", name)
	}

	if err := c.putSSHKeyLines(kept); err != nil {
		return fmt.Errorf("failed to update SSH keys: %w", err)
	}
	return nil
}
