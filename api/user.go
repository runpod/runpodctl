package api

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

type SSHKey struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

func GetPublicSSHKeys() (string, []SSHKey, error) {
	input := Input{
		Query: `
		query myself {
			myself {
				id
				pubKey
			}
		}
		`,
	}

	res, err := Query(input)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return "", nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var data UserOut
	if err := json.Unmarshal(rawData, &data); err != nil {
		return "", nil, fmt.Errorf("JSON unmarshal error: %w", err)
	}

	if len(data.Errors) > 0 {
		return "", nil, fmt.Errorf("API error: %s", data.Errors[0].Message)
	}

	if data.Data == nil || data.Data.Myself == nil {
		return "", nil, fmt.Errorf("nil data received: %s", string(rawData))
	}

	// Parse the public key string into a list of SSHKey structs
	var keys []SSHKey
	keyStrings := strings.Split(data.Data.Myself.PubKey, "\n")
	for _, keyString := range keyStrings {
		if keyString == "" {
			continue
		}

		pubKey, name, _, _, err := ssh.ParseAuthorizedKey([]byte(keyString))
		if err != nil {
			continue // Skip keys that can't be parsed
		}

		keys = append(keys, SSHKey{
			Name:        name,
			Type:        pubKey.Type(),
			Key:         string(ssh.MarshalAuthorizedKey(pubKey)),
			Fingerprint: ssh.FingerprintSHA256(pubKey),
		})
	}

	return data.Data.Myself.PubKey, keys, nil
}

func AddPublicSSHKey(key []byte) error {
	rawKeys, existingKeys, err := GetPublicSSHKeys()
	if err != nil {
		return fmt.Errorf("failed to get existing SSH keys: %w", err)
	}

	keyStr := string(key)
	for _, k := range existingKeys {
		if strings.TrimSpace(k.Key) == strings.TrimSpace(keyStr) {
			return nil
		}
	}

	// Concatenate the new key onto the existing keys, separated by a newline
	newKeys := strings.TrimSpace(rawKeys)
	if newKeys != "" {
		newKeys += "\n\n"
	}
	newKeys += strings.TrimSpace(keyStr)

	input := Input{
		Query: `
		mutation Mutation($input: UpdateUserSettingsInput) {
			updateUserSettings(input: $input) {
			  id
			}
		  }
		`,
		Variables: map[string]interface{}{"input": map[string]interface{}{"pubKey": newKeys}},
	}

	if _, err = Query(input); err != nil {
		return fmt.Errorf("failed to update SSH keys: %w", err)
	}

	return nil
}
