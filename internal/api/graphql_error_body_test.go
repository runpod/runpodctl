package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// TestGraphQLRejectedStatusKeepsStatusWhenBodyUnreadable: a received error status
// whose body is oversized or cut short must still surface as an APIError carrying
// the status and its stable code. graphql is sent once, so the count stays 1.
func TestGraphQLRejectedStatusKeepsStatusWhenBodyUnreadable(t *testing.T) {
	bodies := map[string]func(*testing.T, http.ResponseWriter, int){
		"oversized": writeOversized,
		"truncated": writeTruncated,
	}
	codes := map[int]string{http.StatusTooManyRequests: "rate_limited", http.StatusServiceUnavailable: "server_error"}
	for status, wantCode := range codes {
		for bodyName, write := range bodies {
			t.Run(strconv.Itoa(status)+"/"+bodyName, func(t *testing.T) {
				withFastRetry(t)
				var n atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					n.Add(1)
					write(t, w, status)
				}))
				t.Cleanup(server.Close)

				client := newTestGraphQLClient(t, server.URL)
				_, err := client.Query(GraphQLInput{Query: "query { myself { id } }"})
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected an *APIError, got %T: %v", err, err)
				}
				if apiErr.Status != status {
					t.Fatalf("status = %d, want %d", apiErr.Status, status)
				}
				if got := apiErr.ErrorCode(); got != wantCode {
					t.Fatalf("code = %q, want %q", got, wantCode)
				}
				if !strings.Contains(apiErr.Message, "status "+strconv.Itoa(status)+" (the error body could not be read:") {
					t.Fatalf("message = %q", apiErr.Message)
				}
				if got := n.Load(); got != 1 {
					t.Fatalf("attempts = %d, want 1", got)
				}
			})
		}
	}
}

// a body that cannot be read on a 200 is still the read error, not an APIError.
func TestGraphQLSuccessStatusWithUnreadableBodyStaysReadError(t *testing.T) {
	for name, write := range map[string]func(*testing.T, http.ResponseWriter, int){
		"oversized": writeOversized,
		"truncated": writeTruncated,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				write(t, w, http.StatusOK)
			}))
			t.Cleanup(server.Close)

			client := newTestGraphQLClient(t, server.URL)
			_, err := client.Query(GraphQLInput{Query: "query { myself { id } }"})
			if err == nil {
				t.Fatal("expected a read error")
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) { //nolint:modernize // errors.As, not AsType: nix errcheck flags AsType
				t.Fatalf("a 2xx read failure must not be an APIError: %v", err)
			}
		})
	}
}
