package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// TestClientsRefuseInsecureAPIURL pins that the rest client and the typed
// graphql client both refuse a non-loopback http:// api url before sending the
// key, while loopback http (httptest, a local dev stack) keeps working.
func TestClientsRefuseInsecureAPIURL(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Cleanup(func() { configenv.SetAllowInsecureAPI(false) })
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv(configenv.AllowInsecureAPIEnv, "")
	configenv.SetAllowInsecureAPI(false)

	t.Run("rest", func(t *testing.T) {
		t.Setenv("RUNPOD_API_URL", "http://api.example.invalid")
		client, err := NewClient()
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if _, err := client.GetV2("/pods", nil); !errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("expected ErrInsecureAPIURL, got %v", err)
		}
	})

	t.Run("graphql", func(t *testing.T) {
		t.Setenv("RUNPOD_GRAPHQL_URL", "http://api.example.invalid/graphql")
		client, err := NewGraphQLClient()
		if err != nil {
			t.Fatalf("NewGraphQLClient: %v", err)
		}
		if _, err := client.Query(GraphQLInput{Query: "query test"}); !errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("expected ErrInsecureAPIURL, got %v", err)
		}
	})

	t.Run("loopback http still works", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{}`)); err != nil {
				t.Errorf("write: %v", err)
			}
		}))
		defer server.Close()
		t.Setenv("RUNPOD_API_URL", server.URL)
		client, err := NewClient()
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if _, err := client.GetV2("/pods", nil); errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("loopback http must be allowed, got %v", err)
		}
	})
}

// TestClientsRejectOversizeResponses pins the response bound on the rest client
// and on the typed graphql client.
func TestClientsRejectOversizeResponses(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	chunk := []byte(strings.Repeat("x", 1<<20))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range (MaxResponseBytes >> 20) + 2 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	t.Setenv("RUNPOD_API_URL", server.URL)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	rest, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, restErr := rest.GetV2("/pods", nil); restErr == nil || !strings.Contains(restErr.Error(), "exceeds") {
		t.Errorf("rest: expected an over-limit error, got %v", restErr)
	}

	gql, err := NewGraphQLClient()
	if err != nil {
		t.Fatalf("NewGraphQLClient: %v", err)
	}
	if _, gqlErr := gql.Query(GraphQLInput{Query: "query test"}); gqlErr == nil || !strings.Contains(gqlErr.Error(), "exceeds") {
		t.Errorf("graphql: expected an over-limit error, got %v", gqlErr)
	}
}
