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

// TestWriteSecureConfig_TightensExistingDir covers an upgrade on an existing
// install: a ~/.runpod created by an older version (or a loose umask) at 0755
// or 0777 must be clamped to 0700, not left wide. os.MkdirAll leaves an
// existing directory's mode untouched, so WriteSecureConfig must chmod it.
func TestWriteSecureConfig_TightensExistingDir(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("apiKey", "secret-value")

	dir := filepath.Join(t.TempDir(), ".runpod")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}

	if err := WriteSecureConfig(dir); err != nil {
		t.Fatalf("WriteSecureConfig: %v", err)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %#o, want 0700 after re-save on an existing dir", perm)
	}
}

// TestEnsureSecureFile_ClampsBeforeWrite pins the window-closing behavior: the
// config file is clamped to 0600 before any secret content is written, so the
// secret is never present in a group/other-readable file even briefly. It
// tightens a pre-existing loose file in place without truncating its contents.
func TestEnsureSecureFile_ClampsBeforeWrite(t *testing.T) {
	dir := t.TempDir()

	// Missing file: created owner-only.
	missing := filepath.Join(dir, "new.toml")
	if err := ensureSecureFile(missing); err != nil {
		t.Fatalf("ensureSecureFile(missing): %v", err)
	}
	fi, err := os.Stat(missing)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("new file perm = %#o, want 0600", perm)
	}

	// Pre-existing 0644 file: clamped to 0600 with contents preserved (proves
	// the clamp happens in place, before any rewrite).
	existing := filepath.Join(dir, "old.toml")
	if err := os.WriteFile(existing, []byte("apiKey='old'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSecureFile(existing); err != nil {
		t.Fatalf("ensureSecureFile(existing): %v", err)
	}
	fi, err = os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("existing file perm = %#o, want 0600", perm)
	}
	got, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "apiKey='old'\n" {
		t.Errorf("contents = %q, want preserved", string(got))
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
