package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/runpod/runpodctl/internal/clierr"
	"golang.org/x/crypto/ssh"
)

// SSH keys live on the account: rest v2 lists them with GET /account/ssh-keys
// and replaces the whole set with PUT. Add and remove are read-modify-write.

// v2SSHKeyPattern is the pattern rest v2 validates every entry of a key list
// against. one entry that fails it makes v2 reject the whole PUT.
var v2SSHKeyPattern = regexp.MustCompile(`^(ssh|ecdsa|sk)-[^\s]+ [^\s]+([ \t][^\n\r]*)?$`)

// validV2SSHKey reports whether v2 will accept line as a key list entry.
func validV2SSHKey(line string) bool {
	if !v2SSHKeyPattern.MatchString(line) {
		return false
	}
	_, ok := parseSSHKey(line)
	return ok
}

// InvalidStoredSSHKeysError is an account key list holding entries v2 rejects,
// left over from when keys were one free-text blob. while they are there, no
// key can be added or removed, so the cli stops before writing and says how
// to clear them.
type InvalidStoredSSHKeysError struct {
	Entries []string
}

func (e *InvalidStoredSSHKeysError) Error() string {
	return fmt.Sprintf("your account's ssh key list has %d saved entr%s that %s not a valid public key (first: %q). "+
		"rest v2 rejects the whole list while %s there, so no key can be added or removed. "+
		"re-run with --remove-invalid to delete %s as part of this command, run 'runpodctl ssh remove-key --invalid', "+
		"or remove %s in the console under settings, ssh public keys",
		len(e.Entries), plural(len(e.Entries), "y", "ies"), plural(len(e.Entries), "is", "are"), truncateForError(e.Entries[0]),
		plural(len(e.Entries), "it is", "they are"), plural(len(e.Entries), "it", "them"), plural(len(e.Entries), "it", "them"))
}

// ErrorCode is conflict: an account state no retry fixes.
func (e *InvalidStoredSSHKeysError) ErrorCode() string { return "conflict" }

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// checkStoredSSHKeys returns lines ready to send: with removeInvalid, minus the
// entries v2 rejects; without it, an InvalidStoredSSHKeysError if there are any,
// since v2 would reject the whole list.
func checkStoredSSHKeys(lines []string, removeInvalid bool) ([]string, error) {
	var invalid []string
	kept := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" && !validV2SSHKey(strings.TrimSpace(line)) {
			invalid = append(invalid, line)
			continue
		}
		kept = append(kept, line)
	}
	if len(invalid) > 0 && !removeInvalid {
		return nil, &InvalidStoredSSHKeysError{Entries: invalid}
	}
	return kept, nil
}

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

// putSSHKeyLines replaces the account's key list. entries go out trimmed and
// blank ones are dropped: v2 checks each entry against an anchored pattern
// before it stores anything, so a stray leading space or an empty leftover
// from the old blob would fail the whole list. v2 trims and drops them on
// storage anyway, so nothing a user saved changes.
func (c *Client) putSSHKeyLines(lines []string) error {
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			clean = append(clean, line)
		}
	}
	_, err := c.PutV2("/account/ssh-keys", &sshKeysBody{Keys: clean})
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
// block of several keys is split. removeInvalid drops saved entries v2 rejects
// (see InvalidStoredSSHKeysError) in the same write instead of refusing.
func (c *Client) AddPublicSSHKey(key []byte, removeInvalid bool) error {
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
		// blank lines and comments, as in an authorized_keys file
		if newKey == "" || strings.HasPrefix(newKey, "#") {
			continue
		}
		// v2 validates every entry of the list it is sent, so one line that
		// isn't a key would fail the whole update; refuse it up front.
		parsed, ok := parseSSHKey(newKey)
		if !ok || !validV2SSHKey(newKey) {
			return clierr.Usagef("not an ssh public key: %q", truncateForError(newKey))
		}
		if have[parsed.Fingerprint] {
			continue
		}
		have[parsed.Fingerprint] = true
		lines = append(lines, newKey)
		added = true
	}
	if !added {
		return nil
	}
	lines, err = checkStoredSSHKeys(lines, removeInvalid)
	if err != nil {
		return err
	}
	if err := c.putSSHKeyLines(lines); err != nil {
		return fmt.Errorf("failed to update SSH keys: %w", err)
	}
	return nil
}

// RemovePublicSSHKey removes the key matching name and/or fingerprint. a name
// matching several keys is ambiguous and needs the fingerprint. removeInvalid
// is as for AddPublicSSHKey.
func (c *Client) RemovePublicSSHKey(name, fingerprint string, removeInvalid bool) error {
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
	kept, err = checkStoredSSHKeys(kept, removeInvalid)
	if err != nil {
		return err
	}

	if err := c.putSSHKeyLines(kept); err != nil {
		return fmt.Errorf("failed to update SSH keys: %w", err)
	}
	return nil
}

// truncateForError shortens a line for an error message, so a pasted private
// key or a long blob isn't echoed back in full.
func truncateForError(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// RemoveInvalidSSHKeys deletes the key list entries v2 rejects (see
// InvalidStoredSSHKeysError) and keeps every valid key. it returns how many
// entries it removed; with none, it writes nothing.
func (c *Client) RemoveInvalidSSHKeys() (int, error) {
	lines, err := c.getSSHKeyLines()
	if err != nil {
		return 0, fmt.Errorf("failed to get existing SSH keys: %w", err)
	}
	kept := []string{}
	removed := 0
	for _, line := range lines {
		switch line = strings.TrimSpace(line); {
		case line == "": // dropped by any write, not a saved entry
		case validV2SSHKey(line):
			kept = append(kept, line)
		default:
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	if err := c.putSSHKeyLines(kept); err != nil {
		return 0, fmt.Errorf("failed to update SSH keys: %w", err)
	}
	return removed, nil
}
