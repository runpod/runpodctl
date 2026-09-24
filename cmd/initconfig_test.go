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
	// ReadInConfig keeps the previous map when it fails, so without a reset a
	// test can pass on keys an earlier one loaded.
	resetViper(t)
	t.Cleanup(func() { resetViper(t) })
	outputFormat = "json"
	t.Setenv("APIKEY", "")
	home := t.TempDir()
	// USERPROFILE covers windows, where os.UserHomeDir reads that instead.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// resetViper clears the global viper and restores the bindings cmd/config makes
// at package init, which viper.Reset drops.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	for _, name := range []string{"apiKey", "apiUrl"} {
		if err := viper.BindPFlag(name, config.ConfigCmd.Flags().Lookup(name)); err != nil {
			t.Fatalf("bind %s: %v", name, err)
		}
	}
	viper.SetDefault("apiKey", "")
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

// TestInitConfigFollowsSymlinks covers dotfile managers (stow, home-manager) and
// configs linked into /workspace on a pod: the link is followed, the target is
// narrowed, and the command still runs.
func TestInitConfigFollowsSymlinks(t *testing.T) {
	for _, name := range []string{".runpod/config.toml", ".runpod.yaml"} {
		t.Run(name, func(t *testing.T) {
			home := tempHome(t)
			target := filepath.Join(t.TempDir(), "target")
			content := "apikey: 'linked-key'\n"
			if strings.HasSuffix(name, ".toml") {
				content = "apikey = 'linked-key'\n"
			}
			writeFile(t, target, content, 0o644)
			symlinkOrSkip(t, target, filepath.Join(home, name))

			stderr := captureStderr(t, initConfig)
			assertConfigInitAllowed(t)

			if strings.Contains(stderr, "warning") {
				t.Errorf("stderr = %q, want no warnings", stderr)
			}
			assertMode(t, target, configFilePerm)
			if got := viper.GetString("apiKey"); got != "linked-key" {
				t.Errorf("apiKey = %q, want linked-key", got)
			}
		})
	}
}

func TestInitConfigFollowsSymlinkedDirectory(t *testing.T) {
	home := tempHome(t)
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, target, filepath.Join(home, ".runpod"))

	initConfig()
	assertConfigInitAllowed(t)

	assertMode(t, target, configDirPerm)
	assertMode(t, filepath.Join(target, "config.toml"), configFilePerm)
}

// TestInitConfigWarnsOnUnexpectedFiles: a path that is not the expected kind is
// reported and left alone, and never stops the command.
func TestInitConfigWarnsOnUnexpectedFiles(t *testing.T) {
	for _, testCase := range []struct {
		path string
		link bool
	}{
		{".runpod/config.toml", false},
		{".runpod/config.toml", true},
		{".runpod.yaml", false},
		{".runpod.yaml", true},
	} {
		name := testCase.path + "/directory"
		if testCase.link {
			name += " symlink"
		}
		t.Run(name, func(t *testing.T) {
			home := tempHome(t)
			path := filepath.Join(home, testCase.path)
			if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
				t.Fatal(err)
			}
			target := path
			if testCase.link {
				target = filepath.Join(t.TempDir(), "target")
			}
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if testCase.link {
				symlinkOrSkip(t, target, path)
			}

			stderr := captureStderr(t, initConfig)
			assertConfigInitAllowed(t)

			if !strings.Contains(stderr, "unexpected config file type") {
				t.Errorf("stderr = %q, want an unexpected type warning", stderr)
			}
			assertMode(t, target, 0o755)
		})
	}
}

// TestInitConfigWarnsOnUnreadableLegacy: a legacy yaml that exists but cannot be
// parsed is named on stderr rather than silently replaced by a defaults toml.
func TestInitConfigWarnsOnUnreadableLegacy(t *testing.T) {
	home := tempHome(t)
	legacyFile := filepath.Join(home, ".runpod.yaml")
	writeFile(t, legacyFile, "apiKey: [unclosed\n", 0o600)

	stderr := captureStderr(t, initConfig)
	assertConfigInitAllowed(t)

	if !strings.Contains(stderr, "could not read "+legacyFile) {
		t.Errorf("stderr = %q, want a warning naming %s", stderr, legacyFile)
	}
	if strings.Contains(stderr, "migrating config") {
		t.Errorf("stderr = %q, want no migration notice", stderr)
	}
}

func symlinkOrSkip(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), configDirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func assertConfigInitAllowed(t *testing.T) {
	t.Helper()
	if configInitErr != nil {
		t.Fatalf("init error = %v, want none", configInitErr)
	}
	if err := rootCmd.PersistentPreRunE(config.ConfigCmd, nil); err != nil {
		t.Fatalf("config pre-run error = %v, want none", err)
	}
}

// TestInitConfigWarnsOnPermissionErrors covers read-only, foreign-owned and
// image-baked configs: a failed inspect, repair or create is reported on stderr
// and the command still runs, so RUNPOD_API_KEY keeps working.
func TestInitConfigWarnsOnPermissionErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires unix permissions")
	}
	for _, testCase := range []struct {
		kind, want string
	}{
		{"stat", "could not inspect"},
		{"repair", "could not open"},
		{"create", "could not create"},
	} {
		t.Run(testCase.kind, func(t *testing.T) {
			home := tempHome(t)
			configDir := filepath.Join(home, ".runpod")
			configFile := filepath.Join(configDir, "config.toml")
			blocked, mode := configDir, os.FileMode(0)
			switch testCase.kind {
			case "repair":
				writeFile(t, configFile, "apikey = 'unchanged'\n", configFilePerm)
				blocked, mode = configFile, 0o044
			case "create":
				if err := os.MkdirAll(configDir, configDirPerm); err != nil {
					t.Fatal(err)
				}
				mode = 0o500
			default:
				writeFile(t, configFile, "apikey = 'unchanged'\n", configFilePerm)
			}
			if err := os.Chmod(blocked, mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(blocked, 0o700); err != nil {
					t.Errorf("restore fixture permissions: %v", err)
				}
			})
			if testCase.kind == "create" {
				if err := os.WriteFile(configFile, nil, configFilePerm); !errors.Is(err, fs.ErrPermission) {
					t.Skipf("permissions are not enforced for this user: %v", err)
				}
			} else if _, err := os.ReadFile(configFile); !errors.Is(err, fs.ErrPermission) {
				t.Skipf("permissions are not enforced for this user: %v", err)
			}

			stderr := captureStderr(t, initConfig)
			assertConfigInitAllowed(t)

			if !strings.Contains(stderr, testCase.want) {
				t.Errorf("stderr = %q, want a warning containing %q", stderr, testCase.want)
			}
		})
	}
}
