package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestGetPodsRequestsAndParsesRuntime pins the two things the runtimeStatus
// derivation depends on: that the myPods query actually asks for the runtime
// block (a silently dropped field would make every running pod look like it was
// still initializing), and that a null runtime decodes to a nil pointer rather
// than an empty struct.
func TestGetPodsRequestsAndParsesRuntime(t *testing.T) {
	const responseBody = `{"data":{"myself":{"pods":[
		{"id":"up","desiredStatus":"RUNNING","lastStatusChange":"Rented by User: x",
		 "runtime":{"uptimeInSeconds":111,
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

	for _, field := range []string{"runtime", "uptimeInSeconds", "ports", "isIpPublic", "privatePort", "publicPort"} {
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
	if len(up.Runtime.Ports) != 1 || up.Runtime.Ports[0].PrivatePort != 22 {
		t.Errorf("ports not parsed: %+v", up.Runtime.Ports)
	}

	// null runtime must stay a nil pointer: that absence is the signal.
	if pods[1].Runtime != nil {
		t.Errorf("expected nil runtime for the initializing pod, got %+v", pods[1].Runtime)
	}
}

// TestLimitTimeout pins the `pod list` runtime-probe cap. Nothing else
// constrains it: `pod list` had never touched graphql before CON-690, so if this
// only-ever-shortens rule silently stops applying, an unresponsive graphql turns
// a ~100ms list into a 30s stall for the sake of a decorative field.
func TestLimitTimeout(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	newClient := func(t *testing.T) *GraphQLClient {
		t.Helper()
		client, err := NewGraphQLClient()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.httpClient.Timeout != 30*time.Second {
			t.Fatalf("expected the 30s default, got %v", client.httpClient.Timeout)
		}
		return client
	}

	t.Run("shortens", func(t *testing.T) {
		client := newClient(t)
		client.LimitTimeout(5 * time.Second)
		if client.httpClient.Timeout != 5*time.Second {
			t.Errorf("cap did not apply: got %v, want 5s", client.httpClient.Timeout)
		}
	})

	t.Run("never lengthens", func(t *testing.T) {
		client := newClient(t)
		client.LimitTimeout(60 * time.Second)
		if client.httpClient.Timeout != 30*time.Second {
			t.Errorf("cap lengthened the timeout: got %v, want 30s", client.httpClient.Timeout)
		}
	})

	t.Run("ignores non-positive", func(t *testing.T) {
		client := newClient(t)
		client.LimitTimeout(0)
		client.LimitTimeout(-time.Second)
		if client.httpClient.Timeout != 30*time.Second {
			t.Errorf("got %v, want the 30s default untouched", client.httpClient.Timeout)
		}
	})

	t.Run("keeps a tighter operator setting", func(t *testing.T) {
		viper.Set("graphqlTimeout", 2*time.Second)
		t.Cleanup(func() { viper.Set("graphqlTimeout", 0) })
		client, err := NewGraphQLClient()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		client.LimitTimeout(5 * time.Second)
		if client.httpClient.Timeout != 2*time.Second {
			t.Errorf("clobbered a tighter configured timeout: got %v, want 2s", client.httpClient.Timeout)
		}
	})
}
