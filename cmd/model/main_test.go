package model

import (
	"os"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
)

// TestMain opts the package's tests into plain-http upload urls. Most upload
// tests run against an httptest server on http://127.0.0.1 with the production
// api url configured, a combination the upload url check refuses by design; the
// tests that pin that check clear the variable through setUploadHTTPPolicy.
func TestMain(m *testing.M) {
	if err := os.Setenv(configenv.AllowInsecureAPIEnv, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// setUploadHTTPPolicy sets the inputs of modelUploadHTTPAllowed for one test:
// whether --insecure / RUNPOD_ALLOW_INSECURE_API is on, and the configured
// graphql api url ("" means the production default).
func setUploadHTTPPolicy(t *testing.T, optIn bool, apiURL string) {
	t.Helper()
	env := ""
	if optIn {
		env = "1"
	}
	t.Setenv(configenv.AllowInsecureAPIEnv, env)
	t.Setenv(configenv.GraphQLURLEnv, apiURL)
	configenv.SetAllowInsecureAPI(false)
	t.Cleanup(func() { configenv.SetAllowInsecureAPI(false) })
}
