package cmd

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/cmd/config"
	"github.com/spf13/viper"
)

func tempHome(t *testing.T) string {
	t.Helper()
	flag := config.ConfigCmd.Flags().Lookup("apiKey")
	savedValue, savedChanged := flag.Value.String(), flag.Changed
	savedInitErr, savedFormat := configInitErr, outputFormat
	t.Cleanup(func() {
		if err := flag.Value.Set(savedValue); err != nil {
			t.Errorf("restore api key flag: %v", err)
		}
		flag.Changed = savedChanged
		configInitErr, outputFormat = savedInitErr, savedFormat
	})
	if err := flag.Value.Set(""); err != nil {
		t.Fatalf("clear api key flag: %v", err)
	}
	flag.Changed = false
	outputFormat = "json"
	t.Setenv("APIKEY", "")
	home := t.TempDir()
	// USERPROFILE covers windows, where os.UserHomeDir reads that instead.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
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

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	saved := os.Stderr
	defer func() {
		os.Stderr = saved
		reader.Close()
		writer.Close()
	}()
	os.Stderr = writer
	fn()
	os.Stderr = saved
	if err := writer.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}
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
	if configInitErr != nil {
		t.Fatal(configInitErr)
	}

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
	if configInitErr != nil {
		t.Fatal(configInitErr)
	}

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
	if err := viper.WriteConfig(); err != nil {
		t.Fatalf("overwrite config: %v", err)
	}
	assertMode(t, configFile, configFilePerm)
	if got, err := os.ReadFile(configFile); err != nil || !strings.Contains(string(got), "secret-abc") {
		t.Errorf("overwritten config = %q (err %v), want stored key", got, err)
	}
}

func TestInitConfigMigratesLegacyConfig(t *testing.T) {
	home := tempHome(t)
	configFile := filepath.Join(home, ".runpod", "config.toml")
	legacyFile := filepath.Join(home, ".runpod.yaml")

	writeFile(t, legacyFile, "apiKey: legacy-key-123\n", 0o644)

	stderr := captureStderr(t, initConfig)
	if configInitErr != nil {
		t.Fatal(configInitErr)
	}

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
	if configInitErr != nil {
		t.Fatal(configInitErr)
	}

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
	if configInitErr != nil {
		t.Fatal(configInitErr)
	}

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

func TestInitConfigRejectsUnexpectedFiles(t *testing.T) {
	for _, testCase := range []struct {
		path string
		kind string
	}{
		{".runpod/config.toml", "directory"},
		{".runpod/config.toml", "file symlink"},
		{".runpod/config.toml", "directory symlink"},
		{".runpod.yaml", "directory"},
		{".runpod.yaml", "file symlink"},
		{".runpod.yaml", "directory symlink"},
	} {
		t.Run(testCase.path+"/"+testCase.kind, func(t *testing.T) {
			home := tempHome(t)
			configDir := filepath.Join(home, ".runpod")
			target, mode := unexpectedConfigFile(t, filepath.Join(home, testCase.path), testCase.kind)

			initConfig()

			assertConfigInitBlocked(t)
			assertMode(t, target, mode)
			if testCase.kind == "file symlink" {
				assertConfigFileContents(t, target, "apikey = 'unchanged'\n")
			}
			if testCase.path == ".runpod.yaml" {
				if _, err := os.Stat(filepath.Join(configDir, "config.toml")); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("config created despite unsafe legacy path: %v", err)
				}
			}
		})
	}
}

func assertConfigFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("config contents = %q, want %q", got, want)
	}
}

func unexpectedConfigFile(t *testing.T, path, kind string) (string, os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	target := path
	if kind != "directory" {
		target = filepath.Join(t.TempDir(), "target")
	}
	mode := os.FileMode(0o755)
	if kind == "file symlink" {
		mode = 0o644
		writeFile(t, target, "apikey = 'unchanged'\n", mode)
	} else {
		if err := os.Mkdir(target, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, mode); err != nil {
			t.Fatal(err)
		}
	}
	if target != path {
		if err := os.Symlink(target, path); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("symlinks unavailable: %v", err)
			}
			t.Fatal(err)
		}
	}
	return target, mode
}

func assertConfigInitBlocked(t *testing.T) {
	t.Helper()
	if configInitErr == nil {
		t.Fatal("expected unsafe config path to fail initialization")
	}
	if err := rootCmd.PersistentPreRunE(config.ConfigCmd, nil); !errors.Is(err, configInitErr) {
		t.Fatalf("config pre-run error = %v, want %v", err, configInitErr)
	}
}

func TestInitConfigRejectsSymlinkedDirectory(t *testing.T) {
	home := tempHome(t)
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, ".runpod")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}

	initConfig()

	assertConfigInitBlocked(t)
	assertMode(t, target, 0o755)
	if _, err := os.Stat(filepath.Join(target, "config.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("config created through symlink: %v", err)
	}
}

func TestInitConfigRejectsPermissionErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires unix permissions")
	}
	for _, kind := range []string{"stat", "repair"} {
		t.Run(kind, func(t *testing.T) {
			home := tempHome(t)
			configDir := filepath.Join(home, ".runpod")
			configFile := filepath.Join(configDir, "config.toml")
			writeFile(t, configFile, "apikey = 'unchanged'\n", configFilePerm)
			blocked, mode := configDir, os.FileMode(0)
			if kind == "repair" {
				blocked, mode = configFile, 0o044
			}
			if err := os.Chmod(blocked, mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(blocked, 0o700); err != nil {
					t.Errorf("restore fixture permissions: %v", err)
				}
			})
			if _, err := os.ReadFile(configFile); !errors.Is(err, fs.ErrPermission) {
				t.Skipf("permissions are not enforced for this user: %v", err)
			}

			initConfig()

			if !errors.Is(configInitErr, fs.ErrPermission) {
				t.Fatalf("init error = %v, want permission error", configInitErr)
			}
			if err := rootCmd.PersistentPreRunE(config.ConfigCmd, nil); !errors.Is(err, fs.ErrPermission) {
				t.Fatalf("config pre-run error = %v, want permission error", err)
			}
		})
	}
}
