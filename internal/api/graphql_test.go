package api

import (
	"testing"

	"github.com/spf13/viper"
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

func TestNewGraphQLClientEnvOverridesConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("apiKey", "config-key")
	viper.Set("apiUrl", "https://config.example.test/graphql")
	t.Setenv("RUNPOD_API_KEY", "env-key")
	t.Setenv("RUNPOD_GRAPHQL_URL", "https://env.example.test/graphql")

	client, err := NewGraphQLClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.apiKey != "env-key" {
		t.Fatalf("expected env api key, got %q", client.apiKey)
	}
	if client.url != "https://env.example.test/graphql" {
		t.Fatalf("expected env graphql url, got %q", client.url)
	}
}
