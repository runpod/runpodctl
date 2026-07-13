package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
