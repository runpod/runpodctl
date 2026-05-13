package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSHKeyMatches(t *testing.T) {
	key := SSHKey{
		Name:        "temp-key",
		Fingerprint: "SHA256:test",
	}

	if !sshKeyMatches(key, "temp-key", "") {
		t.Fatal("expected name match")
	}
	if !sshKeyMatches(key, "", "SHA256:test") {
		t.Fatal("expected fingerprint match")
	}
	if !sshKeyMatches(key, "temp-key", "SHA256:test") {
		t.Fatal("expected combined match")
	}
	if sshKeyMatches(key, "", "") {
		t.Fatal("expected empty selector not to match")
	}
	if sshKeyMatches(key, "other", "") {
		t.Fatal("expected wrong name not to match")
	}
	if sshKeyMatches(key, "", "SHA256:other") {
		t.Fatal("expected wrong fingerprint not to match")
	}
}

func TestSplitSSHKeyBlock(t *testing.T) {
	keys := splitSSHKeyBlock("\nssh-ed25519 aaa first\n\nssh-rsa bbb second\n")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] != "ssh-ed25519 aaa first" {
		t.Fatalf("unexpected first key: %q", keys[0])
	}
	if keys[1] != "ssh-rsa bbb second" {
		t.Fatalf("unexpected second key: %q", keys[1])
	}
}

func TestRemovePublicSSHKey_ByName(t *testing.T) {
	const (
		keyOne = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBt1lsGGT0o42If0D6v0gk6r4oeKXH7D7x7qSWv8eQzG first-key"
		keyTwo = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP4F5wuS0nPf3B1L6xQ3K6Y1sY1R9e6lV2YxWw8P4v8K keep-key"
	)

	var updatedPubKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input GraphQLInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		switch {
		case strings.Contains(input.Query, "query myself"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"myself": map[string]interface{}{
						"pubKey": keyOne + "\n\n" + keyTwo,
					},
				},
			})
		case strings.Contains(input.Query, "mutation Mutation"):
			pubKey, _ := input.Variables["input"].(map[string]interface{})["pubKey"].(string)
			updatedPubKey = pubKey
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"updateUserSettings": map[string]interface{}{"id": "user-1"},
				},
			})
		default:
			t.Fatalf("unexpected query: %s", input.Query)
		}
	}))
	defer server.Close()

	client := &GraphQLClient{
		url:        server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
		userAgent:  "test",
	}

	if err := client.RemovePublicSSHKey("first-key", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(updatedPubKey, "first-key") {
		t.Fatalf("expected first key to be removed, got %q", updatedPubKey)
	}
	if !strings.Contains(updatedPubKey, "keep-key") {
		t.Fatalf("expected second key to remain, got %q", updatedPubKey)
	}
}

func TestRemovePublicSSHKey_AmbiguousName(t *testing.T) {
	const duplicateKeys = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBt1lsGGT0o42If0D6v0gk6r4oeKXH7D7x7qSWv8eQzG temp-key\n\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP4F5wuS0nPf3B1L6xQ3K6Y1sY1R9e6lV2YxWw8P4v8K temp-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myself": map[string]interface{}{
					"pubKey": duplicateKeys,
				},
			},
		})
	}))
	defer server.Close()

	client := &GraphQLClient{
		url:        server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
		userAgent:  "test",
	}

	err := client.RemovePublicSSHKey("temp-key", "")
	if err == nil {
		t.Fatal("expected ambiguous name error")
	}
	if !strings.Contains(err.Error(), "multiple ssh keys found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
