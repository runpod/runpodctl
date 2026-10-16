package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func newTestGraphQLClient(t *testing.T, url string) *GraphQLClient {
	t.Helper()
	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_GRAPHQL_URL", url)
	c, err := NewGraphQLClient()
	if err != nil {
		t.Fatalf("NewGraphQLClient: %v", err)
	}
	c.url = url
	return c
}

// TestGraphQLIsNeverRetried pins that a GraphQL call is sent exactly once. every
// call is a POST, and a 429 or 5xx does not prove a mutation did nothing (the
// server can rate limit a follow-up read after podCreate already ran), so a
// retry could bill a second pod. reads are not told apart from mutations: a
// wrong guess costs money, and a skipped retry only costs a rerun.
func TestGraphQLIsNeverRetried(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		status int
	}{
		{"mutation 429", "mutation { podFindAndDeployOnDemand { id } }", http.StatusTooManyRequests},
		{"mutation 502", "mutation { podFindAndDeployOnDemand { id } }", http.StatusBadGateway},
		{"query 429", "query { myself { id } }", http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFastRetry(t)
			var n atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)

			client := newTestGraphQLClient(t, server.URL)
			if _, err := client.Query(GraphQLInput{Query: tt.query}); err == nil {
				t.Fatalf("expected error on %d", tt.status)
			}
			if got := n.Load(); got != 1 {
				t.Fatalf("expected exactly 1 attempt, got %d", got)
			}
		})
	}
}
