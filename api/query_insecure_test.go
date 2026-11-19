package api

import (
	"errors"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// TestQueryRefusesInsecureAPIURL pins that the graphql client will not send the
// api key to a non-loopback http:// url, from env or config, and that the
// documented opt-ins lift the refusal (the request then fails later, on the
// network, not with ErrInsecureAPIURL).
func TestQueryRefusesInsecureAPIURL(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Cleanup(func() { configenv.SetAllowInsecureAPI(false) })
	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set(GraphQLTimeoutKey, "1s")

	t.Run("env url", func(t *testing.T) {
		t.Setenv(configenv.AllowInsecureAPIEnv, "")
		t.Setenv("RUNPOD_GRAPHQL_URL", "http://api.example.invalid/graphql")
		configenv.SetAllowInsecureAPI(false)
		if err := queryErr(t); !errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("expected ErrInsecureAPIURL, got %v", err)
		}
	})

	t.Run("config url", func(t *testing.T) {
		t.Setenv(configenv.AllowInsecureAPIEnv, "")
		t.Setenv("RUNPOD_GRAPHQL_URL", "")
		viper.Set("apiUrl", "http://api.example.invalid/graphql")
		t.Cleanup(func() { viper.Set("apiUrl", "") })
		configenv.SetAllowInsecureAPI(false)
		if err := queryErr(t); !errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("expected ErrInsecureAPIURL, got %v", err)
		}
	})

	t.Run("env opt-in", func(t *testing.T) {
		t.Setenv(configenv.AllowInsecureAPIEnv, "1")
		t.Setenv("RUNPOD_GRAPHQL_URL", "http://api.example.invalid/graphql")
		configenv.SetAllowInsecureAPI(false)
		if err := queryErr(t); errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("expected opt-in to lift the refusal, got %v", err)
		}
	})

	t.Run("flag opt-in", func(t *testing.T) {
		t.Setenv(configenv.AllowInsecureAPIEnv, "")
		t.Setenv("RUNPOD_GRAPHQL_URL", "http://api.example.invalid/graphql")
		configenv.SetAllowInsecureAPI(true)
		if err := queryErr(t); errors.Is(err, configenv.ErrInsecureAPIURL) {
			t.Fatalf("expected --insecure to lift the refusal, got %v", err)
		}
	})
}

// queryErr issues a query and returns only its error, closing any response body
// so the opt-in cases (which reach the network) do not leak it.
func queryErr(t *testing.T) error {
	t.Helper()
	res, err := Query(Input{Query: "query test"})
	if res != nil {
		if closeErr := res.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
	}
	return err
}
