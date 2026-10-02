package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestParseAPIError(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		status   int
		wantMsg  string
		wantCode string
	}{
		{"nested error envelope is unwrapped", `{"error":"pod not found"}`, 404, "pod not found", "not_found"},
		{"message envelope is unwrapped", `{"message":"bad request"}`, 400, "bad request", "bad_request"},
		{"explicit code is preserved", `{"error":"denied","code":"quota_exceeded"}`, 403, "denied", "quota_exceeded"},
		{"raw non-json body is used verbatim", "internal error", 500, "internal error", "server_error"},
		{"empty body falls back to status message", "", 502, "api request failed with status 502", "server_error"},
		{"rest v2 validation errors are appended to detail", `{"detail":"Request validation failed.","errors":["$: missing property 'image'","$: additional properties 'imageName' not allowed"],"status":422,"title":"Unprocessable Entity"}`, 422, "Request validation failed. $: missing property 'image'; $: additional properties 'imageName' not allowed", "bad_request"},
		{"rest v2 detail without errors is used alone", `{"detail":"pod not found","status":404,"title":"Not Found"}`, 404, "pod not found", "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := parseAPIError([]byte(tt.body), tt.status)
			if e.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", e.Message, tt.wantMsg)
			}
			if e.ErrorCode() != tt.wantCode {
				t.Errorf("code = %q, want %q", e.ErrorCode(), tt.wantCode)
			}
			if e.HTTPStatus() != tt.status {
				t.Errorf("status = %d, want %d", e.HTTPStatus(), tt.status)
			}
			// the whole point: the message must not be a double-encoded json blob.
			if strings.Contains(e.Message, `{"error"`) || strings.Contains(e.Message, "(status ") {
				t.Errorf("message is still double-encoded: %q", e.Message)
			}
		})
	}
}

func TestParseAPIError_ImplementsError(t *testing.T) {
	var err error = parseAPIError([]byte(`{"error":"nope"}`), 404)
	if err.Error() != "nope" {
		t.Errorf("Error() = %q, want 'nope'", err.Error())
	}
}

func TestGraphQLError_Shape(t *testing.T) {
	e := newGraphQLError("something broke")
	if e.Error() != "graphql error: something broke" {
		t.Errorf("Error() = %q", e.Error())
	}
	if e.ErrorCode() != "graphql_error" {
		t.Errorf("ErrorCode() = %q, want graphql_error", e.ErrorCode())
	}
}

func TestParseGraphQLHTTPError(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		status  int
		wantMsg string
	}{
		{"errors envelope is unwrapped", `{"errors":[{"message":"bad token"}]}`, 401, "bad token"},
		{"error envelope is unwrapped", `{"error":"nope"}`, 400, "nope"},
		{"raw non-json body", "gateway timeout", 504, "gateway timeout"},
		{"empty body falls back to status", "", 502, "request failed with status 502"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := parseGraphQLHTTPError([]byte(tt.body), tt.status)
			if e.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", e.Message, tt.wantMsg)
			}
			if e.HTTPStatus() != tt.status {
				t.Errorf("status = %d, want %d", e.HTTPStatus(), tt.status)
			}
			if e.ErrorCode() != "graphql_error" {
				t.Errorf("code = %q, want graphql_error", e.ErrorCode())
			}
			// must not double-encode the raw envelope back into the message.
			if strings.Contains(e.Message, `{"error`) {
				t.Errorf("message still double-encoded: %q", e.Message)
			}
		})
	}
}

func TestNewNotFoundError(t *testing.T) {
	err := NewNotFoundError("template not found: %s", "tpl-1")
	if got := err.Error(); got != "template not found: tpl-1" {
		t.Errorf("message = %q", got)
	}
	if got := err.ErrorCode(); got != "not_found" {
		t.Errorf("code = %q, want not_found", got)
	}
	// graphql returns 200 with null data, so no wire status should be invented.
	if got := err.HTTPStatus(); got != 0 {
		t.Errorf("status = %d, want 0", got)
	}
	// must survive wrapping, since callers wrap with fmt.Errorf("failed to …: %w").
	wrapped := fmt.Errorf("failed to get template: %w", err)
	var apiErr *APIError
	if !errors.As(wrapped, &apiErr) || apiErr.ErrorCode() != "not_found" {
		t.Errorf("wrapped error lost its code: %v", wrapped)
	}
}

func TestCodeForStatus(t *testing.T) {
	// pins the full vocabulary the runpodctl agent skill documents (CON-752).
	tests := []struct {
		status int
		want   string
	}{
		{400, "bad_request"},
		{401, "unauthorized"},
		{403, "forbidden"},
		{404, "not_found"},
		{409, "conflict"},
		{429, "rate_limited"},
		{500, "server_error"},
		{502, "server_error"},
		{503, "server_error"},
		{418, "api_error"},   // any other non-zero status
		{422, "bad_request"}, // rest v2 validation failure
		{0, ""},              // no status to derive from
	}
	for _, tt := range tests {
		if got := codeForStatus(tt.status); got != tt.want {
			t.Errorf("codeForStatus(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestParseAPIError_NeverEmitsANestedJSONBlob(t *testing.T) {
	// a non-2xx body that is valid json but carries no error/message key used to
	// be dumped verbatim into the message, reproducing the double-encoded shape
	// this package exists to remove and contradicting the documented contract.
	tests := []struct {
		name    string
		body    string
		status  int
		wantMsg string
	}{
		{"unknown json object falls back to the status", `{"detail2":"x","field":"gpuIds"}`, 400, "api request failed with status 400"},
		{"json array falls back to the status", `[{"loc":["body"],"msg":"bad"}]`, 422, "api request failed with status 422"},
		{"detail is used when present", `{"detail":"validation failed"}`, 400, "validation failed"},
		{"plain text is still used verbatim", "upstream timeout", 504, "upstream timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAPIError([]byte(tt.body), tt.status)
			if got.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", got.Message, tt.wantMsg)
			}
			if strings.HasPrefix(got.Message, "{") || strings.HasPrefix(got.Message, "[") {
				t.Errorf("message must never be a raw json blob, got %q", got.Message)
			}
		})
	}
}

func TestRequestV2UsesV2BaseURL(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &Client{
		baseURL:    "http://v1.invalid",
		v2BaseURL:  server.URL + "/v2/",
		apiKey:     "test-key",
		httpClient: server.Client(),
	}
	if _, err := client.GetV2("/pods", url.Values{"limit": {"5"}}); err != nil {
		t.Fatalf("GetV2: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/v2/pods" || gotQuery != "limit=5" {
		t.Fatalf("got %s %s?%s, want GET /v2/pods?limit=5", gotMethod, gotPath, gotQuery)
	}
	if _, err := client.DeleteV2("/pods/abc"); err != nil {
		t.Fatalf("DeleteV2: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/v2/pods/abc" {
		t.Fatalf("got %s %s, want DELETE /v2/pods/abc", gotMethod, gotPath)
	}
}

func TestV2URLFollowsConfig(t *testing.T) {
	t.Setenv("RUNPOD_REST_V2_URL", "https://v2.example.test/v2/")
	if got := (&Client{}).v2URL(); got != "https://v2.example.test/v2" {
		t.Fatalf("v2URL = %q, want the configured url without a trailing slash", got)
	}
}
