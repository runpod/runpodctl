package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// presignedLocation carries the credentials a redirect to a presigned url would:
// a SigV4 signature and a bearer token in the query.
//
//nolint:gosec // G101: fake credential fixtures
const (
	leakSignature = "0123456789abcdef0123456789abcdef"
	leakToken     = "tok-leak-9f8e7d6c"
	leakLocation  = "https://bucket.example.invalid/obj?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=" + leakSignature + "&access_token=Bearer%20" + leakToken
)

func leakingRedirectServer(t *testing.T) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, leakLocation, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

// assertNoQueryCredentials checks every rendering of err, and every *url.Error
// reachable through its chain, for the secret query values.
func assertNoQueryCredentials(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error for a redirected request")
	}
	renderings := map[string]string{
		"Error()": err.Error(),
		"%v":      fmt.Sprintf("%v", err),
		"%+v":     fmt.Sprintf("%+v", err),
		"%#v":     fmt.Sprintf("%#v", err),
	}
	var walk func(e error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if ue, ok := e.(*url.Error); ok { //nolint:errorlint // every node of the chain is inspected on purpose
			renderings["url.Error.URL"] += ue.URL + "\n"
		}
		switch u := e.(type) { //nolint:errorlint // walking the chain by hand
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, c := range u.Unwrap() {
				walk(c)
			}
		}
	}
	walk(err)
	if ue, ok := errors.AsType[*url.Error](err); ok {
		renderings["errors.As url.Error.URL"] = ue.URL
	}
	for how, text := range renderings {
		for _, secret := range []string{leakSignature, leakToken, "X-Amz-Signature", "access_token"} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaks %q: %s", how, secret, text)
			}
		}
	}
}

// TestRedirectToPresignedURLLeaksNoQueryCredentials sends every api-key client
// at a server that redirects to a presigned url and checks that nothing in the
// resulting error carries the url's query.
func TestRedirectToPresignedURLLeaksNoQueryCredentials(t *testing.T) {
	for name, call := range map[string]func(t *testing.T, base string) error{
		"graphql client": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_GRAPHQL_URL", base+"/graphql")
			c, err := NewGraphQLClient()
			if err != nil {
				t.Fatalf("NewGraphQLClient: %v", err)
			}
			_, err = c.Query(GraphQLInput{Query: "query test"})
			return err
		},
		"rest client GET": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_API_URL", base)
			c, err := NewClient()
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.GetV2("/pods", nil)
			return err
		},
		"rest client POST": func(t *testing.T, base string) error {
			t.Setenv("RUNPOD_API_URL", base)
			c, err := NewClient()
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.PostV2("/pods", map[string]string{"name": "x"})
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
			assertNoQueryCredentials(t, call(t, leakingRedirectServer(t)))
		})
	}
}
