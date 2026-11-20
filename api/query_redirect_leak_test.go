package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

//nolint:gosec // G101: fake credential fixtures
const (
	leakSignature = "0123456789abcdef0123456789abcdef"
	leakToken     = "tok-leak-9f8e7d6c"
	leakLocation  = "https://bucket.example.invalid/obj?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=" + leakSignature + "&access_token=Bearer%20" + leakToken
)

// TestQueryRedirectToPresignedURLLeaksNoQueryCredentials: a redirect to a
// presigned url must not put its query in the error of the model-repo graphql
// path, in any rendering or in a *url.Error recovered with errors.As.
func TestQueryRedirectToPresignedURLLeaksNoQueryCredentials(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "super-secret-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, leakLocation, http.StatusFound)
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := CompleteModelRepoUploadBatch([]string{"session-a"})
	if err == nil {
		t.Fatal("expected an error for a redirected request")
	}
	renderings := []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)}
	var ue *url.Error
	if errors.As(err, &ue) {
		renderings = append(renderings, ue.URL, ue.Error())
	}
	for _, text := range renderings {
		for _, secret := range []string{leakSignature, leakToken, "X-Amz-Signature", "access_token"} {
			if strings.Contains(text, secret) {
				t.Errorf("error leaks %q: %s", secret, text)
			}
		}
	}

	res, qerr := Query(Input{Query: "query test"})
	if qerr == nil {
		if closeErr := res.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
		t.Fatal("expected Query to fail on a redirect")
	}
	for _, text := range []string{qerr.Error(), fmt.Sprintf("%+v", qerr)} {
		if strings.Contains(text, leakSignature) || strings.Contains(text, leakToken) {
			t.Errorf("Query error leaks credentials: %s", text)
		}
	}
	if errors.As(qerr, &ue) && (strings.Contains(ue.URL, leakSignature) || strings.Contains(ue.URL, "?")) {
		t.Errorf("Query error url.Error.URL keeps the query: %s", ue.URL)
	}
}
