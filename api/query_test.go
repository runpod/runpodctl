package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/spf13/viper"
)

func TestQueryUsesGraphQLEndpointEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	restServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected query not to use RUNPOD_API_URL")
	}))
	defer restServer.Close()
	t.Setenv("RUNPOD_API_URL", restServer.URL)

	configServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected RUNPOD_GRAPHQL_URL to override configured apiUrl")
	}))
	defer configServer.Close()
	viper.Set("apiUrl", configServer.URL)

	graphqlServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if input.Query != "query test" {
			t.Fatalf("unexpected query: %q", input.Query)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{}})
	}))
	defer graphqlServer.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", graphqlServer.URL)

	res, err := Query(Input{Query: "query test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
}

func TestQueryFallsBackToConfiguredAPIURL(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{}})
	}))
	defer server.Close()
	viper.Set("apiUrl", server.URL)

	res, err := Query(Input{Query: "query test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()
}

// TestQueryMalformedURLErrorIsRedacted pins that a request-construction
// failure (a *url.Error quoting the raw url) does not echo credentials from
// the url query string, in the message or anywhere in the chain.
func TestQueryMalformedURLErrorIsRedacted(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_GRAPHQL_URL", "https://bad host/x?X-Amz-Signature=deadbeef0123456789")

	res, err := Query(Input{Query: "query test"})
	if res != nil {
		defer func() {
			if closeErr := res.Body.Close(); closeErr != nil {
				t.Errorf("close response body: %v", closeErr)
			}
		}()
	}
	if err == nil {
		t.Fatal("expected a request-construction error")
	}
	if strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("error leaks the url query: %v", err)
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("expected a *url.Error in the chain, got %T: %v", err, err)
	}
	if strings.Contains(urlErr.URL, "deadbeef") {
		t.Fatalf("the url.Error in the chain still carries the query: %v", urlErr.URL)
	}
}

func TestErrNoCredentialsIsMatchableFromThisPackage(t *testing.T) {
	// api is a public package, so callers outside the module must be able to
	// errors.Is against the sentinel rather than string-matching the message.
	// It must also be the SAME value the rest client returns, so one check covers
	// every path.
	t.Setenv("RUNPOD_API_KEY", "")
	viper.Reset()
	t.Cleanup(viper.Reset)

	_, err := Query(Input{Query: "{ myself { id } }"})
	if err == nil {
		t.Fatal("expected an error with no api key configured")
	}
	if !errors.Is(err, ErrNoCredentials) {
		t.Errorf("errors.Is(err, ErrNoCredentials) = false; err = %v", err)
	}
	if !errors.Is(err, internalapi.ErrNoCredentials) {
		t.Error("the public sentinel must be the same value as internal/api's")
	}
	var apiErr *internalapi.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "no_credentials" {
		t.Errorf("expected code no_credentials, got %v", err)
	}
}
