package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestInitConfig_WritesSecurePermissions pins that the config directory and the
// config file initConfig creates are not world-readable. The file holds the api
// key (persisted via config --apiKey / doctor), so a 0644 file inside a 0777
// directory would leak the secret to other users on a shared host. The two
// creation sites in initConfig previously disagreed (os.ModePerm here vs 0700 in
// doctor), and neither chmod'd the file below viper's default 0644.
func TestInitConfig_WritesSecurePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows: os.UserHomeDir reads this

	// isolate global viper state from other tests in the package
	viper.Reset()
	t.Cleanup(viper.Reset)

	// no config file exists in the fresh temp home, so initConfig takes the
	// write path.
	initConfig()

	dir := filepath.Join(home, ".runpod")
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm = %#o, want 0700", perm)
	}

	file := filepath.Join(dir, "config.toml")
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file perm = %#o, want 0600 (secret must not be world-readable)", perm)
	}
}
