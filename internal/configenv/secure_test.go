package configenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// TestWriteSecureConfig_Permissions pins the security invariant for the file
// that stores the api key: the directory is owner-only (0700) and the file is
// not readable by group/other (0600). Regressing either — e.g. back to
// os.ModePerm on the dir or viper's default 0644 on the file — leaks the secret
// on a shared host.
func TestWriteSecureConfig_Permissions(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("apiKey", "secret-value")

	dir := filepath.Join(t.TempDir(), ".runpod")
	if err := WriteSecureConfig(dir); err != nil {
		t.Fatalf("WriteSecureConfig: %v", err)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %#o, want 0700", perm)
	}

	file := filepath.Join(dir, "config.toml")
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %#o, want 0600", perm)
	}
}

// TestWriteSecureConfig_TightensExistingFile covers the doctor/config re-save
// path: a pre-existing world-readable config file must be clamped back to 0600,
// not left as it was found.
func TestWriteSecureConfig_TightensExistingFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := filepath.Join(t.TempDir(), ".runpod")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, []byte("apiKey='old'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteSecureConfig(dir); err != nil {
		t.Fatalf("WriteSecureConfig: %v", err)
	}

	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %#o, want 0600 after re-save", perm)
	}
}
