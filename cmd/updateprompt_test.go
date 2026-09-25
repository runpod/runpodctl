package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type updatePromptHarness struct {
	statePath string
	now       time.Time
	fetches   int
	installs  int
}

// newUpdatePromptHarness swaps every seam so tests never touch the network,
// the real ~/.runpod, or the terminal.
func newUpdatePromptHarness(t *testing.T, currentVersion string) *updatePromptHarness {
	t.Helper()
	h := &updatePromptHarness{
		statePath: filepath.Join(t.TempDir(), "update-check.json"),
		now:       time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}
	for _, env := range []string{noUpdateCheckEnv, "CI", "RUNPOD_POD_ID"} {
		t.Setenv(env, "")
	}

	oldVersion, oldPath, oldInteractive := version, updateStatePath, updateInteractive
	oldFetch, oldInstall, oldNow := fetchLatestVersion, installUpdate, updateNow
	t.Cleanup(func() {
		version, updateStatePath, updateInteractive = oldVersion, oldPath, oldInteractive
		fetchLatestVersion, installUpdate, updateNow = oldFetch, oldInstall, oldNow
		pendingUpdateCheck = nil
	})

	version = currentVersion
	updateStatePath = func() string { return h.statePath }
	updateInteractive = func() bool { return true }
	fetchLatestVersion = func(context.Context) (string, error) {
		h.fetches++
		return "v2.14.0", nil
	}
	installUpdate = func(io.Writer) error {
		h.installs++
		return nil
	}
	updateNow = func() time.Time { return h.now }
	return h
}

func subcommand() *cobra.Command {
	root := &cobra.Command{Use: "runpodctl"}
	pod := &cobra.Command{Use: "pod"}
	root.AddCommand(pod)
	return pod
}

func TestParseUpdateAnswer(t *testing.T) {
	tests := map[string]updateAnswer{
		"y":       answerYes,
		"Y":       answerYes,
		" yes\r":  answerYes,
		"s":       answerSkip,
		"SKIP":    answerSkip,
		"":        answerLater,
		"n":       answerLater,
		"no":      answerLater,
		"maybe":   answerLater,
		"yes plz": answerLater,
	}
	for in, want := range tests {
		if got := parseUpdateAnswer(in); got != want {
			t.Errorf("parseUpdateAnswer(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestReadLineLeavesRestOfInput(t *testing.T) {
	in := strings.NewReader("y\nsecret-api-key\n")
	if got := readLine(in); got != "y" {
		t.Fatalf("readLine = %q, want %q", got, "y")
	}
	rest, _ := io.ReadAll(in)
	if string(rest) != "secret-api-key\n" {
		t.Fatalf("remaining input = %q, want the next line untouched", rest)
	}
}

func TestReleaseSemver(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{in: "2.14.0-dd55bcf", want: "v2.14.0", wantOK: true},
		{in: "v2.14.0", want: "v2.14.0", wantOK: true},
		{in: "2.14.0", want: "v2.14.0", wantOK: true},
		{in: "2.0.0-beta.1-abc1234", want: "v2.0.0-beta.1", wantOK: true},
		{in: "dev-abc1234", wantOK: false},
		{in: "", wantOK: false},
	}
	for _, tt := range tests {
		got, ok := releaseSemver(tt.in)
		if ok != tt.wantOK || got != tt.want {
			t.Errorf("releaseSemver(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestShouldPromptUpdate(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		state updateState
		want  bool
	}{
		{name: "newer release, never prompted", state: updateState{LatestVersion: "v2.14.0"}, want: true},
		{name: "already on latest", state: updateState{LatestVersion: "v2.13.0"}, want: false},
		{name: "no check yet", state: updateState{}, want: false},
		{name: "skipped this version", state: updateState{LatestVersion: "v2.14.0", SkippedVersion: "v2.14.0"}, want: false},
		{name: "skipped an older version", state: updateState{LatestVersion: "v2.14.0", SkippedVersion: "v2.13.5"}, want: true},
		{name: "prompted an hour ago", state: updateState{LatestVersion: "v2.14.0", PromptedAt: now.Add(-time.Hour)}, want: false},
		{name: "prompted yesterday", state: updateState{LatestVersion: "v2.14.0", PromptedAt: now.Add(-25 * time.Hour)}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPromptUpdate(tt.state, "v2.13.0", now); got != tt.want {
				t.Fatalf("shouldPromptUpdate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdatePromptEnabled(t *testing.T) {
	root := &cobra.Command{Use: "runpodctl"}
	pod := &cobra.Command{Use: "pod"}
	update := &cobra.Command{Use: "update"}
	complete := &cobra.Command{Use: "__complete"}
	root.AddCommand(pod, update, complete)

	tests := []struct {
		name        string
		cmd         *cobra.Command
		env         string
		interactive bool
		want        bool
	}{
		{name: "interactive subcommand", cmd: pod, interactive: true, want: true},
		{name: "not a terminal", cmd: pod, interactive: false, want: false},
		{name: "opted out", cmd: pod, env: noUpdateCheckEnv, interactive: true, want: false},
		{name: "ci", cmd: pod, env: "CI", interactive: true, want: false},
		{name: "on a pod", cmd: pod, env: "RUNPOD_POD_ID", interactive: true, want: false},
		{name: "update command", cmd: update, interactive: true, want: false},
		{name: "shell completion", cmd: complete, interactive: true, want: false},
		{name: "bare root", cmd: root, interactive: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newUpdatePromptHarness(t, "2.13.0-abc1234")
			if tt.env != "" {
				t.Setenv(tt.env, "1")
			}
			updateInteractive = func() bool { return tt.interactive }
			if got := updatePromptEnabled(tt.cmd); got != tt.want {
				t.Fatalf("updatePromptEnabled = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFirstRunChecksInBackgroundAndPromptsNextRun(t *testing.T) {
	h := newUpdatePromptHarness(t, "2.13.0-abc1234")
	var out bytes.Buffer

	maybePromptUpdate(subcommand(), strings.NewReader(""), &out)
	if out.Len() != 0 {
		t.Fatalf("first run printed %q, want no prompt before a check completes", out.String())
	}
	waitForUpdateCheck()
	if h.fetches != 1 {
		t.Fatalf("fetches = %d, want 1", h.fetches)
	}

	h.now = h.now.Add(time.Minute)
	maybePromptUpdate(subcommand(), strings.NewReader("\n"), &out)
	if !strings.Contains(out.String(), "runpodctl v2.14.0 is available (you have v2.13.0)") {
		t.Fatalf("second run output = %q, want the update prompt", out.String())
	}
	if h.fetches != 1 {
		t.Fatalf("fetches = %d, want no second check within a day", h.fetches)
	}
}

func TestPromptAnswers(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantInstalls int
		wantSkipped  string
	}{
		{name: "yes installs", input: "y\n", wantInstalls: 1},
		{name: "enter means not now", input: "\n", wantInstalls: 0},
		{name: "unknown input means not now", input: "what\n", wantInstalls: 0},
		{name: "eof means not now", input: "", wantInstalls: 0},
		{name: "skip records the version", input: "s\n", wantSkipped: "v2.14.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newUpdatePromptHarness(t, "2.13.0-abc1234")
			saveUpdateState(h.statePath, updateState{CheckedAt: h.now, LatestVersion: "v2.14.0"})

			var out bytes.Buffer
			maybePromptUpdate(subcommand(), strings.NewReader(tt.input), &out)

			if h.installs != tt.wantInstalls {
				t.Fatalf("installs = %d, want %d", h.installs, tt.wantInstalls)
			}
			state := loadUpdateState(h.statePath)
			if state.SkippedVersion != tt.wantSkipped {
				t.Fatalf("skipped = %q, want %q", state.SkippedVersion, tt.wantSkipped)
			}
			if !state.PromptedAt.Equal(h.now) {
				t.Fatalf("prompted_at = %v, want %v", state.PromptedAt, h.now)
			}

			// any answer silences the prompt for the rest of the day
			out.Reset()
			h.now = h.now.Add(time.Hour)
			maybePromptUpdate(subcommand(), strings.NewReader("y\n"), &out)
			if out.Len() != 0 {
				t.Fatalf("re-prompted within a day: %q", out.String())
			}
		})
	}
}

func TestInstallFailureDoesNotStopCommand(t *testing.T) {
	h := newUpdatePromptHarness(t, "2.13.0-abc1234")
	saveUpdateState(h.statePath, updateState{CheckedAt: h.now, LatestVersion: "v2.14.0"})
	installUpdate = func(io.Writer) error { return errors.New("brew not found") }

	var out bytes.Buffer
	maybePromptUpdate(subcommand(), strings.NewReader("y\n"), &out)
	if !strings.Contains(out.String(), "update failed: brew not found") {
		t.Fatalf("output = %q, want the failure reported", out.String())
	}
}

func TestFailedCheckKeepsLastKnownVersion(t *testing.T) {
	h := newUpdatePromptHarness(t, "2.13.0-abc1234")
	saveUpdateState(h.statePath, updateState{CheckedAt: h.now.Add(-48 * time.Hour), LatestVersion: "v2.14.0", PromptedAt: h.now})
	fetchLatestVersion = func(context.Context) (string, error) { return "", errors.New("rate limited") }

	maybePromptUpdate(subcommand(), strings.NewReader(""), io.Discard)
	waitForUpdateCheck()

	state := loadUpdateState(h.statePath)
	if state.LatestVersion != "v2.14.0" {
		t.Fatalf("latest = %q, want the last known version kept", state.LatestVersion)
	}
	if !state.CheckedAt.Equal(h.now) {
		t.Fatalf("checked_at = %v, want the failed check recorded so it is not retried every run", state.CheckedAt)
	}
}

func TestDevBuildNeverPrompts(t *testing.T) {
	h := newUpdatePromptHarness(t, "dev-abc1234")
	saveUpdateState(h.statePath, updateState{CheckedAt: h.now, LatestVersion: "v2.14.0"})

	var out bytes.Buffer
	maybePromptUpdate(subcommand(), strings.NewReader("y\n"), &out)
	if out.Len() != 0 || h.fetches != 0 {
		t.Fatalf("dev build prompted (%q) or fetched (%d)", out.String(), h.fetches)
	}
}

func TestPackageManagerUpdate(t *testing.T) {
	condaPrefix := t.TempDir()
	if err := os.MkdirAll(filepath.Join(condaPrefix, "conda-meta"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		exe  string
		want []string
	}{
		{name: "brew formula", exe: "/opt/homebrew/Cellar/runpodctl/2.14.0/bin/runpodctl", want: []string{"brew", "upgrade", "runpodctl"}},
		{name: "brew cask", exe: "/opt/homebrew/Caskroom/runpodctl/2.14.0/runpodctl", want: []string{"brew", "upgrade", "--cask", "runpodctl"}},
		{name: "pixi global", exe: "/home/u/.pixi/envs/runpodctl/bin/runpodctl", want: []string{"pixi", "global", "update", "runpodctl"}},
		{name: "conda", exe: filepath.Join(condaPrefix, "bin", "runpodctl"), want: []string{"conda", "update", "-y", "-p", condaPrefix, "runpodctl"}},
		{name: "conda windows", exe: filepath.Join(condaPrefix, "Library", "bin", "runpodctl.exe"), want: []string{"conda", "update", "-y", "-p", condaPrefix, "runpodctl"}},
		{name: "direct download", exe: filepath.Join(t.TempDir(), "bin", "runpodctl"), want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageManagerUpdate(tt.exe); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("packageManagerUpdate(%q) = %v, want %v", tt.exe, got, tt.want)
			}
		})
	}
}

func TestPackageManagerUpdateFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	cellarBin := filepath.Join(dir, "Cellar", "runpodctl", "2.14.0", "bin")
	if err := os.MkdirAll(cellarBin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(cellarBin, "runpodctl")
	if err := os.WriteFile(target, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "runpodctl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	want := []string{"brew", "upgrade", "runpodctl"}
	if got := packageManagerUpdate(link); !reflect.DeepEqual(got, want) {
		t.Fatalf("packageManagerUpdate(symlink) = %v, want %v", got, want)
	}
}
