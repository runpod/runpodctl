package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// redirectPair starts a server that 307-redirects every request to a second
// listener on another port of the same host (the case go forwards the
// Authorization header for) and returns the first url plus a counter of requests
// the second listener received, with the last Authorization header it saw.
func redirectPair(t *testing.T) (first string, hits *atomic.Int32, auth *atomic.Value) {
	t.Helper()
	hits = &atomic.Int32{}
	auth = &atomic.Value{}
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"data":{}}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(second.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)
	return redirector.URL, hits, auth
}

// TestAPIKeyClientsRefuseRedirects pins that every client that sends the api key
// refuses a redirect to another port on the same host: the request fails and the
// second listener never receives it (and so never sees the Authorization header).
func TestAPIKeyClientsRefuseRedirects(t *testing.T) {
	for name, call := range map[string]func(t *testing.T, base string) error{
		"graphql client (template create/update)": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_GRAPHQL_URL", base+"/graphql")
			c, err := NewGraphQLClient()
			if err != nil {
				t.Fatalf("NewGraphQLClient: %v", err)
			}
			_, err = c.Query(GraphQLInput{Query: "query test"})
			if !errors.Is(err, ErrRedirectRefused) {
				t.Errorf("expected ErrRedirectRefused, got %v", err)
			}
			return err
		},
		"rest client": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_API_URL", base)
			c, err := NewClient()
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.GetV2("/pods", nil)
			return err
		},
		"rest v2 client": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_API_URL", base)
			c, err := NewV2Client()
			if err != nil {
				t.Fatalf("NewV2Client: %v", err)
			}
			_, err = c.get(context.Background(), "/endpoints/e/workers")
			return err
		},
		"invoke client": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_INVOKE_URL", base)
			c, err := NewInvokeClient()
			if err != nil {
				t.Fatalf("NewInvokeClient: %v", err)
			}
			_, err = c.EndpointHealth(context.Background(), "ep")
			return err
		},
		"log client": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_API_URL", base)
			c, err := NewLogClient()
			if err != nil {
				t.Fatalf("NewLogClient: %v", err)
			}
			return c.Snapshot(context.Background(), "/pods/p/logs", LogStreamOptions{}, time.Second, func(LogEntry) error { return nil })
		},
	} {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv("RUNPOD_API_KEY", "super-secret-key")
			base, hits, auth := redirectPair(t)

			err := call(t, base)
			if err == nil {
				t.Fatal("expected an error for a redirected request, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "redirect") {
				t.Errorf("error should name the redirect, got %v", err)
			}
			if got := hits.Load(); got != 0 {
				t.Fatalf("redirect target received %d request(s) (Authorization=%v); expected none", got, auth.Load())
			}
		})
	}
}
