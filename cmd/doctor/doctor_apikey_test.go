package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestCheckAPIKey_NonInteractive pins that a key supplied via --api-key is
// persisted without reading stdin, so CI/containers/agents can use the blessed
// `runpodctl doctor` onboarding path non-interactively. Before this, doctor read
// the key only from an interactive os.Stdin prompt.
func TestCheckAPIKey_NonInteractive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("RUNPOD_API_KEY", "") // force the fix path, not env auth

	viper.Reset()
	t.Cleanup(viper.Reset)

	// Redirect stdin to a closed pipe: if the code tries to prompt, it reads
	// EOF and fails, which would make this test fail — proving no prompt path.
	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.Close()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin; r.Close() })

	const key = "rpa_TESTKEY_1234567890"
	result := checkAPIKey(key)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !result.Fixed || result.Status != "pass" {
		t.Fatalf("expected fixed/pass, got fixed=%v status=%q", result.Fixed, result.Status)
	}

	// the key must be persisted to the secure config file
	data, err := os.ReadFile(filepath.Join(home, ".runpod", "config.toml"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), key) {
		t.Errorf("config.toml does not contain the provided key; got:\n%s", data)
	}
}
