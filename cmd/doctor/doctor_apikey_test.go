package doctor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	goodKey = "rpa_GOOD_1234567890"
	oldKey  = "rpa_OLD_1234567890"
	badKey  = "rpa_FAKE"
)

// fakeAPI answers the graphql `myself` query used to validate a key: goodKey
// authenticates, anything else gets the 401 prod returns for an unknown key.
func fakeAPI(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte(`{"error":"unauthorized"}`)); err != nil {
				t.Error(err)
			}
			return
		}
		if _, err := w.Write([]byte(`{"data":{"myself":{"id":"user-1","email":"dev@example.test"}}}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)
}

// isolatedHome points HOME at a temp dir with no env key and a fresh viper, so
// nothing reads or writes the developer's real config. viper.Reset is safe in
// this package: unlike cmd, it binds no pflags in init.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("RUNPOD_API_KEY", "")
	viper.Reset()
	t.Cleanup(viper.Reset)
	fakeAPI(t)
	return home
}

// saveExistingKey writes a config holding key and loads it the way root's
// initConfig does, so the saved-key paths exercise the real WriteConfig target.
func saveExistingKey(t *testing.T, home, key string) {
	t.Helper()
	dir := filepath.Join(home, ".runpod")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, []byte("apikey = '"+key+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(file)
	viper.SetConfigType("toml")
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
}

func configPath(home string) string {
	return filepath.Join(home, ".runpod", "config.toml")
}

func readConfig(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(configPath(home))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	return string(data)
}

func TestCheckAPIKey_FlagSavesValidatedKeyPrivately(t *testing.T) {
	home := isolatedHome(t)
	// an empty stdin: if the flag path prompted, the read would fail the check
	result := checkAPIKey(goodKey, strings.NewReader(""), io.Discard)

	if result.Error != "" || !result.Fixed || result.Status != statusPass {
		t.Fatalf("want fixed pass, got %+v", result)
	}
	if !strings.Contains(readConfig(t, home), goodKey) {
		t.Error("config.toml does not contain the provided key")
	}
	info, err := os.Stat(configPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("config.toml mode = %o, want 600", got)
	}
}

func TestCheckAPIKey_FlagRejectedKeyIsNotSaved(t *testing.T) {
	home := isolatedHome(t)
	result := checkAPIKey(badKey, strings.NewReader(""), io.Discard)

	if result.Status != statusFail || result.Fixed {
		t.Fatalf("want fail, got %+v", result)
	}
	if !strings.Contains(result.Error, "not saved") {
		t.Errorf("error should say the key was not saved, got %q", result.Error)
	}
	data, err := os.ReadFile(configPath(home))
	if err == nil && strings.Contains(string(data), badKey) {
		t.Errorf("rejected key was persisted:\n%s", data)
	}
}

func TestCheckAPIKey_FlagHonoredWhenEnvKeySet(t *testing.T) {
	home := isolatedHome(t)
	t.Setenv("RUNPOD_API_KEY", oldKey)
	result := checkAPIKey(goodKey, strings.NewReader(""), io.Discard)

	if !result.Fixed || result.Status != statusPass {
		t.Fatalf("want fixed pass, got %+v", result)
	}
	if !strings.Contains(readConfig(t, home), goodKey) {
		t.Error("--api-key was ignored because RUNPOD_API_KEY is set")
	}
	if !strings.Contains(result.Warning, "RUNPOD_API_KEY") {
		t.Errorf("want a warning that RUNPOD_API_KEY still takes precedence, got %q", result.Warning)
	}
}

func TestCheckAPIKey_FlagReplacesSavedKey(t *testing.T) {
	home := isolatedHome(t)
	saveExistingKey(t, home, oldKey)
	result := checkAPIKey(goodKey, strings.NewReader(""), io.Discard)

	if !result.Fixed || result.Status != statusPass {
		t.Fatalf("want fixed pass, got %+v", result)
	}
	config := readConfig(t, home)
	if !strings.Contains(config, goodKey) || strings.Contains(config, oldKey) {
		t.Errorf("saved key not replaced:\n%s", config)
	}
	if !strings.Contains(result.Details, "replacing") {
		t.Errorf("want a notice that the saved key was replaced, got %q", result.Details)
	}
}

func TestCheckAPIKey_SavedKeyPassesWithoutFlag(t *testing.T) {
	home := isolatedHome(t)
	saveExistingKey(t, home, oldKey)

	result := checkAPIKey("", strings.NewReader(""), io.Discard)

	if result.Status != statusPass || result.Fixed {
		t.Fatalf("want plain pass, got %+v", result)
	}
}

// `printf %s "$KEY" | runpodctl doctor` sends no trailing newline.
func TestCheckAPIKey_StdinWithoutTrailingNewline(t *testing.T) {
	home := isolatedHome(t)

	result := checkAPIKey("", strings.NewReader(goodKey), io.Discard)

	if result.Error != "" || !result.Fixed {
		t.Fatalf("want fixed, got %+v", result)
	}
	if !strings.Contains(readConfig(t, home), goodKey) {
		t.Error("key read from stdin was not saved")
	}
}

func TestCheckAPIKey_StdinRejectedKeyIsNotSaved(t *testing.T) {
	home := isolatedHome(t)

	result := checkAPIKey("", strings.NewReader(badKey+"\n"), io.Discard)

	if result.Status != statusFail || result.Fixed {
		t.Fatalf("want fail, got %+v", result)
	}
	data, err := os.ReadFile(configPath(home))
	if err == nil && strings.Contains(string(data), badKey) {
		t.Errorf("rejected key was persisted:\n%s", data)
	}
}

func TestCheckAPIKey_EmptyStdinFails(t *testing.T) {
	isolatedHome(t)

	result := checkAPIKey("", strings.NewReader(""), io.Discard)

	if result.Status != statusFail || result.Error == "" {
		t.Fatalf("want fail with an error, got %+v", result)
	}
}

// runs the command through cobra flag parsing, the way the binary does.
func executeDoctor(t *testing.T, args ...string) (string, error) {
	t.Helper()
	// Cmd is a package global, so clear the flag value and its Changed bit
	// or one test's --api-key leaks into the next.
	t.Cleanup(func() {
		apiKeyFlag = ""
		Cmd.Flags().Lookup(keyFlagName).Changed = false
	})
	root := &cobra.Command{Use: "runpodctl", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().StringP("output", "o", "json", "output format")
	root.AddCommand(Cmd)
	root.SetArgs(append([]string{"doctor"}, args...))

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := root.Execute()
	os.Stdout = origStdout
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func TestDoctor_RejectedFlagKeyExitsNonZero(t *testing.T) {
	isolatedHome(t)

	out, err := executeDoctor(t, "--api-key", badKey)

	if err == nil {
		t.Fatal("want an error (non-zero exit) when the supplied key is rejected")
	}
	if !strings.Contains(out, `"healthy": false`) {
		t.Errorf("report should still be printed with healthy false (err %v), got:\n%s", err, out)
	}
}

// `--api-key "$UNSET_VAR"` must not fall through to the stdin read.
func TestDoctor_EmptyFlagIsAnError(t *testing.T) {
	isolatedHome(t)

	_, err := executeDoctor(t, "--api-key", "  ")

	if err == nil || !strings.Contains(err.Error(), "--api-key is empty") {
		t.Fatalf("want an empty --api-key error, got %v", err)
	}
}

// a plain run with a key already saved must also fail loudly when unhealthy,
// so scripts and agents can gate on `runpodctl doctor` alone.
func TestDoctor_UnhealthyWithSavedKeyExitsNonZero(t *testing.T) {
	home := isolatedHome(t)
	saveExistingKey(t, home, oldKey)

	out, err := executeDoctor(t)

	if err == nil {
		t.Fatal("want an error (non-zero exit) when doctor is unhealthy with a saved key")
	}
	if !strings.Contains(out, `"healthy": false`) {
		t.Errorf("report should still be printed with healthy false (err %v), got:\n%s", err, out)
	}
}

// A config that failed to parse at startup leaves viper empty; saving then
// would replace the file with just apiKey and drop everything else in it.
func TestCheckAPIKey_RefusesToOverwriteUnreadableConfig(t *testing.T) {
	home := isolatedHome(t)
	path := filepath.Join(home, ".runpod", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := "[default]\napi_key = \"rpa_SDK\"\nthis is not toml\n"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	result := checkAPIKey(goodKey, strings.NewReader(""), io.Discard)
	if result.Status != statusFail || !strings.Contains(result.Error, "not overwriting") {
		t.Fatalf("expected a refusal to overwrite, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != broken {
		t.Fatalf("config was modified:\n%s", data)
	}
}

// Saving the key rewrites only what the file holds: a value that exists only
// in memory (an env or flag override) must not be persisted.
func TestSaveAPIKey_KeepsFileValuesNotInMemoryOverrides(t *testing.T) {
	home := isolatedHome(t)
	path := filepath.Join(home, ".runpod", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("apiurl = 'https://saved.example'\n[default]\napi_key = 'rpa_SDK'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	viper.Set("apiUrl", "https://override.example")

	if _, err := saveAPIKey(goodKey); err != nil {
		t.Fatal(err)
	}
	written := viper.New()
	written.SetConfigFile(path)
	if err := written.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	if got := written.GetString("apiurl"); got != "https://saved.example" {
		t.Errorf("apiurl = %q, want the saved value", got)
	}
	if got := written.GetString("default.api_key"); got != "rpa_SDK" {
		t.Errorf("[default].api_key = %q, want it kept", got)
	}
	if got := written.GetString("apikey"); got != goodKey {
		t.Errorf("apikey = %q, want %q", got, goodKey)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only config.toml left behind, got %d entries", len(entries))
	}
}

// A symlinked config (dotfile managers) must stay a link, with the key written
// to its target.
func TestSaveAPIKey_WritesThroughSymlink(t *testing.T) {
	home := isolatedHome(t)
	target := filepath.Join(home, "dotfiles", "config.toml")
	link := filepath.Join(home, ".runpod", "config.toml")
	for _, dir := range []string{filepath.Dir(target), filepath.Dir(link)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte("apiurl = 'https://saved.example'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	viper.SetConfigFile(link)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	if _, err := saveAPIKey(goodKey); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config is no longer a symlink: %v", err)
	}
	data, err := os.ReadFile(filepath.Clean(target))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), goodKey) || !strings.Contains(string(data), "saved.example") {
		t.Fatalf("target not updated in place:\n%s", data)
	}
}
