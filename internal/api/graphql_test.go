package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestGetPodsRequestsAndParsesRuntime pins the two things the runtimeStatus
// derivation depends on: that the myPods query actually asks for the runtime
// block (a silently dropped field would make every running pod look like it was
// still initializing), and that a null runtime decodes to a nil pointer rather
// than an empty struct.
func TestGetPodsRequestsAndParsesRuntime(t *testing.T) {
	const responseBody = `{"data":{"myself":{"pods":[
		{"id":"up","desiredStatus":"RUNNING","lastStatusChange":"Rented by User: x",
		 "runtime":{"uptimeInSeconds":111,
			"container":{"cpuPercent":3,"memoryPercent":7},
			"gpus":[{"id":"GPU-1","gpuUtilPercent":11,"memoryUtilPercent":22}],
			"ports":[{"ip":"1.2.3.4","isIpPublic":true,"privatePort":22,"publicPort":40022,"type":"tcp"}]}},
		{"id":"pulling","desiredStatus":"RUNNING","lastStatusChange":"Rented by User: y","runtime":null}
	]}}}`

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input GraphQLInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotQuery = input.Query
		_, _ = w.Write([]byte(responseBody))
	}))
	defer server.Close()

	client := &GraphQLClient{
		url:        server.URL,
		apiKey:     "test-key",
		httpClient: server.Client(),
		userAgent:  "test",
	}

	pods, err := client.GetPods()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, field := range []string{"runtime", "uptimeInSeconds", "container", "cpuPercent", "gpus", "gpuUtilPercent", "ports"} {
		if !strings.Contains(gotQuery, field) {
			t.Errorf("myPods query does not request %q", field)
		}
	}

	if len(pods) != 2 {
		t.Fatalf("expected 2 pods, got %d", len(pods))
	}

	up := pods[0]
	if up.Runtime == nil {
		t.Fatal("expected runtime for the running pod")
	}
	if up.Runtime.UptimeInSeconds == nil || *up.Runtime.UptimeInSeconds != 111 {
		t.Errorf("uptimeInSeconds = %v, want 111", up.Runtime.UptimeInSeconds)
	}
	if up.Runtime.Container == nil || up.Runtime.Container.CPUPercent == nil || *up.Runtime.Container.CPUPercent != 3 {
		t.Errorf("container.cpuPercent not parsed: %+v", up.Runtime.Container)
	}
	if len(up.Runtime.Gpus) != 1 || up.Runtime.Gpus[0].ID != "GPU-1" {
		t.Errorf("gpus not parsed: %+v", up.Runtime.Gpus)
	}
	if len(up.Runtime.Ports) != 1 || up.Runtime.Ports[0].PrivatePort != 22 {
		t.Errorf("ports not parsed: %+v", up.Runtime.Ports)
	}

	// null runtime must stay a nil pointer: that absence is the signal.
	if pods[1].Runtime != nil {
		t.Errorf("expected nil runtime for the initializing pod, got %+v", pods[1].Runtime)
	}
}
