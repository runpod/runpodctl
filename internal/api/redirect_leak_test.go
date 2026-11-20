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
	return redirectServerTo(t, leakLocation)
}

// redirectServerTo starts a server that answers every request with a 307 whose
// Location header is exactly location (http.Redirect would rewrite some forms).
func redirectServerTo(t *testing.T, location string) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

// assertNoQueryCredentials checks err for the secret query values of leakLocation.
func assertNoQueryCredentials(t *testing.T, err error) {
	t.Helper()
	assertNoSecrets(t, err, leakSignature, leakToken, "X-Amz-Signature", "access_token")
}

// assertNoSecrets checks every rendering of err, and every *url.Error reachable
// through its chain, for the given secret strings.
func assertNoSecrets(t *testing.T, err error, secretValues ...string) {
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
	var ue *url.Error
	if errors.As(err, &ue) { //nolint:modernize // errors.As, not AsType: nix errcheck flags AsType
		renderings["errors.As url.Error.URL"] = ue.URL
	}
	for how, text := range renderings {
		for _, secret := range secretValues {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaks %q: %s", how, secret, text)
			}
		}
	}
}

// redirectCallers maps each api-key client to a call that makes one request
// against the server at base and returns the resulting error.
func redirectCallers() map[string]func(t *testing.T, base string) error {
	return map[string]func(t *testing.T, base string) error{
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
	}
}

// TestRedirectToPresignedURLLeaksNoQueryCredentials sends every api-key client
// at a server that redirects to a presigned url and checks that nothing in the
// resulting error carries the url's query.
func TestRedirectToPresignedURLLeaksNoQueryCredentials(t *testing.T) {
	for name, call := range redirectCallers() {
		t.Run(name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv("RUNPOD_API_KEY", "super-secret-key")
			assertNoQueryCredentials(t, call(t, leakingRedirectServer(t)))
		})
	}
}

// TestRedirectToNetworkPathLeaksNoUserinfo redirects every api-key client to a
// network-path reference ("//user:pw@host/...") and a presigned absolute url
// with userinfo: neither the error text nor a typed *url.Error may carry the
// userinfo or the query.
func TestRedirectToNetworkPathLeaksNoUserinfo(t *testing.T) {
	//nolint:gosec // G101: fake credential fixtures
	locations := map[string]string{
		"network path userinfo":           "//user:pw@example.test/x",
		"network path user and query":     "//user@example.test/x?token=abc#frag",
		"absolute with signature":         "https://user:pw@host.test/x?X-Amz-Signature=deadbeef",
		"network path with signature":     "//user:pw@host.test/x?X-Amz-Signature=deadbeef",
		"network path with port and path": "//user:pw@host.test:8443/a/b?token=abc",
	}
	secretValues := []string{"pw", "deadbeef", "abc", "user:", "user@", "token=", "frag", "X-Amz-Signature"}
	for lname, location := range locations {
		for name, call := range redirectCallers() {
			t.Run(lname+"/"+name, func(t *testing.T) {
				viper.Reset()
				t.Cleanup(viper.Reset)
				t.Setenv("RUNPOD_API_KEY", "super-secret-key")
				assertNoSecrets(t, call(t, redirectServerTo(t, location)), secretValues...)
			})
		}
	}
}
