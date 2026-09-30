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

// TestGraphQLQueryRetries429 pins that a rate-limited GraphQL call is retried.
// 429 is safe regardless of query-vs-mutation because the request was rejected
// before processing.
func TestGraphQLQueryRetries429(t *testing.T) {
	withFastRetry(t)
	var n int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)

	client := newTestGraphQLClient(t, server.URL)
	if _, err := client.Query(GraphQLInput{Query: "{ myself { id } }"}); err != nil {
		t.Fatalf("expected success after 429 retry, got %v", err)
	}
	if got := atomic.LoadInt32(&n); got != 2 {
		t.Fatalf("expected 2 attempts after 429, got %d", got)
	}
}

// TestGraphQLQueryDoesNotRetry5xx pins that a GraphQL 5xx is NOT retried: every
// GraphQL call is a POST and the client cannot tell a read from a mutation, so
// retrying could double-execute a mutation (e.g. podCreate).
func TestGraphQLQueryDoesNotRetry5xx(t *testing.T) {
	withFastRetry(t)
	var n int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	client := newTestGraphQLClient(t, server.URL)
	if _, err := client.Query(GraphQLInput{Query: "mutation { podCreate }"}); err == nil {
		t.Fatal("expected error on 502")
	}
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Fatalf("expected exactly 1 attempt on 5xx (no retry), got %d", got)
	}
}
