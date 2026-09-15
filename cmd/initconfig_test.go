package cmd

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// These tests call initConfig directly rather than Execute()ing a command,
// which is the same code path cobra.OnInitialize reaches but without building a
// root. They deliberately do not viper.Reset(): that would drop the --apiKey
// pflag binding cmd/config establishes in its package init, which later tests
// in this package rely on. Determinism instead comes from what is asserted —
// viper's config layer survives a not-found ReadInConfig, so a freshly created
// config may carry keys left by an earlier test, and these tests assert on
// modes, on ConfigFileUsed and on the one key under test rather than on the
// whole file. A successful ReadInConfig replaces that layer outright, so every
// assertion below on a key read back from disk is unaffected by test order.

func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	// USERPROFILE covers windows, where os.UserHomeDir reads that instead.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// no unix mode bits; tightenConfigPermissions returns early there too.
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %#o, want %#o", path, got, want)
	}
}

// captureStderr swaps os.Stderr for a pipe while fn runs. initConfig reports
// every non-fatal problem there, and "warned" versus "silently did nothing" is
// exactly what the malformed-config case turns on. The warnings are far smaller
// than the pipe buffer, so reading after fn returns cannot deadlock.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = saved
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
	r.Close()
	return string(out)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// WriteFile applies the umask, so set the mode explicitly to get the wide
	// modes these tests are about.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

// TestInitConfigCreatesPrivateConfig covers the CON-1160 fix on a fresh machine
// and, with the WriteConfig call at the end, the stale-ConfigFileUsed bug that
// made `runpodctl config --apiKey` fail there with `Config File ".runpod.yaml"
// Not Found`.
func TestInitConfigCreatesPrivateConfig(t *testing.T) {
	home := tempHome(t)
	configDir := filepath.Join(home, ".runpod")
	configFile := filepath.Join(configDir, "config.toml")

	initConfig()

	assertMode(t, configDir, configDirPerm)
	assertMode(t, configFile, configFilePerm)

	if got := viper.ConfigFileUsed(); got != configFile {
		t.Errorf("ConfigFileUsed() = %q, want %q", got, configFile)
	}

	// removing the file first proves WriteConfig recreated *this* path, and that
	// it inherited SetConfigPermissions rather than viper's 0644 default.
	if err := os.Remove(configFile); err != nil {
		t.Fatalf("removing config: %v", err)
	}
	if err := viper.WriteConfig(); err != nil {
		t.Fatalf("WriteConfig after initConfig: %v", err)
	}
	assertMode(t, configFile, configFilePerm)
}

// TestInitConfigTightensExistingModes is the remediation path: MkdirAll never
// changes an existing directory's mode and viper only sets permissions on files
// it creates, so without an explicit chmod every install created before this
// change would keep its 0755 directory and 0644 config.
func TestInitConfigTightensExistingModes(t *testing.T) {
	home := tempHome(t)
	configDir := filepath.Join(home, ".runpod")
	configFile := filepath.Join(configDir, "config.toml")
	legacyFile := filepath.Join(home, ".runpod.yaml")

	const stored = "apikey = 'secret-abc'\n"
	writeFile(t, configFile, stored, 0o644)
	writeFile(t, legacyFile, "apiKey: old-key\n", 0o644)
	if err := os.Chmod(configDir, 0o755); err != nil {
		t.Fatalf("chmod config dir: %v", err)
	}

	initConfig()

	assertMode(t, configDir, configDirPerm)
	assertMode(t, configFile, configFilePerm)
	// the legacy file is remediated but never deleted.
	assertMode(t, legacyFile, configFilePerm)
	if _, err := os.Stat(legacyFile); err != nil {
		t.Errorf("legacy config should survive: %v", err)
	}

	if got := viper.GetString("apiKey"); got != "secret-abc" {
		t.Errorf("apiKey = %q, want secret-abc", got)
	}
	// a readable config must not be rewritten, only re-moded.
	if got, err := os.ReadFile(configFile); err != nil || string(got) != stored {
		t.Errorf("config contents = %q (err %v), want unchanged %q", got, err, stored)
	}
}

func TestInitConfigMigratesLegacyConfig(t *testing.T) {
	home := tempHome(t)
	configFile := filepath.Join(home, ".runpod", "config.toml")
	legacyFile := filepath.Join(home, ".runpod.yaml")

	writeFile(t, legacyFile, "apiKey: legacy-key-123\n", 0o644)

	stderr := captureStderr(t, initConfig)

	if !strings.Contains(stderr, "migrating config") {
		t.Errorf("stderr = %q, want a migration notice", stderr)
	}
	assertMode(t, configFile, configFilePerm)
	assertMode(t, legacyFile, configFilePerm)
	if _, err := os.Stat(legacyFile); err != nil {
		t.Errorf("legacy config should survive migration: %v", err)
	}
	if got := viper.GetString("apiKey"); got != "legacy-key-123" {
		t.Errorf("apiKey = %q, want legacy-key-123", got)
	}
	written, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("reading migrated config: %v", err)
	}
	if !strings.Contains(string(written), "legacy-key-123") {
		t.Errorf("migrated config = %q, want it to carry the legacy key", written)
	}
}

func TestInitConfigPrefersTomlOverLegacy(t *testing.T) {
	home := tempHome(t)
	configFile := filepath.Join(home, ".runpod", "config.toml")
	legacyFile := filepath.Join(home, ".runpod.yaml")

	writeFile(t, configFile, "apikey = 'toml-wins'\n", 0o600)
	writeFile(t, legacyFile, "apiKey: legacy-loses\n", 0o600)

	stderr := captureStderr(t, initConfig)

	if strings.Contains(stderr, "migrating config") {
		t.Errorf("stderr = %q, want no migration when a toml already exists", stderr)
	}
	if got := viper.GetString("apiKey"); got != "toml-wins" {
		t.Errorf("apiKey = %q, want toml-wins", got)
	}
}

// TestInitConfigLeavesMalformedConfigAlone pins the second adjacent bug: the
// migration branch used to be entered on any ReadInConfig error, so a config
// the user could have repaired by hand was force-truncated to defaults, taking
// the stored api key with it.
func TestInitConfigLeavesMalformedConfigAlone(t *testing.T) {
	home := tempHome(t)
	configFile := filepath.Join(home, ".runpod", "config.toml")

	const malformed = "apikey = \n"
	writeFile(t, configFile, malformed, 0o600)

	stderr := captureStderr(t, initConfig)

	if !strings.Contains(stderr, "could not read") {
		t.Errorf("stderr = %q, want a warning naming the unreadable config", stderr)
	}
	got, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if string(got) != malformed {
		t.Errorf("config was rewritten to %q, want it left as %q", got, malformed)
	}
}
