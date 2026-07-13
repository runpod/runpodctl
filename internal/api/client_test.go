package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/agent"
	"github.com/spf13/viper"
)

func TestNewClient_NoAPIKey(t *testing.T) {
	// clear any env vars
	t.Setenv("RUNPOD_API_KEY", "")

	_, err := NewClient()
	if err == nil {
		t.Error("expected error when no api key set")
	}
}

func TestNewClient_WithEnvKey(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Error("expected client to be created")
	}
}

func TestNewClientEnvOverridesConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("apiKey", "config-key")
	viper.Set("restApiUrl", "https://config.example.test")
	t.Setenv("RUNPOD_API_KEY", "env-key")
	t.Setenv("RUNPOD_API_URL", "https://env.example.test")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.apiKey != "env-key" {
		t.Fatalf("expected env api key, got %q", client.apiKey)
	}
	if client.baseURL != "https://env.example.test" {
		t.Fatalf("expected env rest url, got %q", client.baseURL)
	}
}

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected auth header, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected content-type header")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", server.URL)

	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client.baseURL = server.URL

	data, err := client.Get("/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %s", result["status"])
	}
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "new-id"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client.baseURL = server.URL

	data, err := client.Post("/test", map[string]string{"name": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if result["id"] != "new-id" {
		t.Errorf("expected id new-id, got %s", result["id"])
	}
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client.baseURL = server.URL

	_, err = client.Delete("/test/123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	client.baseURL = server.URL

	_, err = client.Get("/notfound", nil)
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestClientGraphQLRequestEnvOverridesConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	configHit := false
	configServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configHit = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer configServer.Close()
	viper.Set("apiUrl", configServer.URL)

	graphqlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myself": map[string]interface{}{
					"id":                "user-1",
					"email":             "test@example.com",
					"clientBalance":     1,
					"currentSpendPerHr": 0,
					"spendLimit":        10,
				},
			},
		})
	}))
	defer graphqlServer.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", graphqlServer.URL)

	client := &Client{
		baseURL:    "https://rest.example.test",
		apiKey:     "test-key",
		httpClient: graphqlServer.Client(),
		userAgent:  "test",
	}

	user, err := client.GetUser()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ID != "user-1" {
		t.Fatalf("unexpected user id: %q", user.ID)
	}
	if configHit {
		t.Fatal("expected RUNPOD_GRAPHQL_URL to override configured apiUrl")
	}
}

func TestBuildUserAgent_Default(t *testing.T) {
	for _, env := range agent.KnownEnvVars() {
		t.Setenv(env, "")
	}
	ua := buildUserAgent()
	if strings.Contains(ua, "(via ") {
		t.Errorf("expected no agent tag, got %s", ua)
	}
	if !strings.HasPrefix(ua, "runpod-cli/") {
		t.Errorf("expected runpod-cli/ prefix, got %s", ua)
	}
}

func TestBuildUserAgent_ClaudeCode(t *testing.T) {
	for _, env := range agent.KnownEnvVars() {
		t.Setenv(env, "")
	}
	t.Setenv("CLAUDECODE", "1")
	ua := buildUserAgent()
	if !strings.Contains(ua, "(via claude-code)") {
		t.Errorf("expected claude-code agent tag, got %s", ua)
	}
}

func TestBuildUserAgent_Codex(t *testing.T) {
	for _, env := range agent.KnownEnvVars() {
		t.Setenv(env, "")
	}
	t.Setenv("CODEX_SANDBOX", "seatbelt")
	ua := buildUserAgent()
	if !strings.Contains(ua, "(via codex)") {
		t.Errorf("expected codex agent tag, got %s", ua)
	}
}

func TestFormatError(t *testing.T) {
	err := FormatError(fmt.Errorf("test error"))
	expected := `{"error":"test error"}`
	if err != expected {
		t.Errorf("expected %s, got %s", expected, err)
	}
}
