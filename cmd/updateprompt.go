package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
	"golang.org/x/term"
)

// startup update prompt. interactive sessions check github for a newer release
// at most once a day, in the background, and the next run offers to install it.
// scripts, agents, ci and pods never see the prompt and never make the request.

const (
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 2 * time.Second
	latestReleaseURL    = "https://api.github.com/repos/runpod/runpodctl/releases/latest"
	noUpdateCheckEnv    = "RUNPOD_NO_UPDATE_CHECK"
)

// updateState is persisted to ~/.runpod/update-check.json between runs.
type updateState struct {
	CheckedAt      time.Time `json:"checked_at"`
	LatestVersion  string    `json:"latest_version,omitempty"`
	PromptedAt     time.Time `json:"prompted_at"`
	SkippedVersion string    `json:"skipped_version,omitempty"`
}

type updateAnswer int

const (
	answerLater updateAnswer = iota
	answerYes
	answerSkip
)

// seams for tests
var (
	updateStatePath    = defaultUpdateStatePath
	updateInteractive  = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())) }
	fetchLatestVersion = fetchLatestReleaseTag
	installUpdate      = installUpdateForChannel
	updateNow          = time.Now
)

// pendingUpdateCheck carries the background check's result to waitForUpdateCheck.
var pendingUpdateCheck chan string

// commands that never prompt: the prompt would be redundant (update, version)
// or would corrupt output a shell consumes (completion, cobra's __complete).
var noUpdatePromptCommands = map[string]bool{
	"update":     true,
	"version":    true,
	"completion": true,
	"help":       true,
}

func updatePromptEnabled(c *cobra.Command) bool {
	if os.Getenv(noUpdateCheckEnv) != "" || os.Getenv("CI") != "" || os.Getenv("RUNPOD_POD_ID") != "" {
		return false
	}
	if c == nil || c == c.Root() || noUpdatePromptCommands[c.Name()] || strings.HasPrefix(c.Name(), "__") {
		return false
	}
	return updateInteractive()
}

// maybePromptUpdate runs from the root PersistentPreRunE. it never returns an
// error: an update problem must not stop the command the user asked for.
func maybePromptUpdate(c *cobra.Command, in io.Reader, out io.Writer) {
	if !updatePromptEnabled(c) {
		return
	}
	current, ok := releaseSemver(version)
	if !ok {
		return
	}

	path := updateStatePath()
	state := loadUpdateState(path)
	now := updateNow()
	if now.Sub(state.CheckedAt) >= updateCheckInterval {
		startUpdateCheck()
	}
	if !shouldPromptUpdate(state, current, now) {
		return
	}

	fmt.Fprintf(out, "runpodctl %s is available (you have %s).\n", state.LatestVersion, current)
	fmt.Fprint(out, "Update now? [y]es / [N]ot now / [s]kip this version: ")
	answer := parseUpdateAnswer(readLine(in))

	state.PromptedAt = now
	switch answer {
	case answerSkip:
		state.SkippedVersion = state.LatestVersion
	case answerYes:
		if err := installUpdate(out); err != nil {
			fmt.Fprintf(out, "update failed: %v\n", err)
		}
	}
	saveUpdateState(path, state)
}

func shouldPromptUpdate(state updateState, current string, now time.Time) bool {
	latest := state.LatestVersion
	if !semver.IsValid(latest) || semver.Compare(current, latest) >= 0 {
		return false
	}
	if latest == state.SkippedVersion {
		return false
	}
	return now.Sub(state.PromptedAt) >= updateCheckInterval
}

// parseUpdateAnswer maps anything other than yes or skip, including an empty
// line or EOF, to "not now" rather than re-asking.
func parseUpdateAnswer(s string) updateAnswer {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return answerYes
	case "s", "skip":
		return answerSkip
	default:
		return answerLater
	}
}

// readLine reads one byte at a time so input typed after the answer stays in
// stdin for the command that runs next.
func readLine(in io.Reader) string {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			b.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}

// releaseCommitSuffix matches the "-<short commit>" goreleaser appends to the
// version (e.g. 2.14.0-dd55bcf).
var releaseCommitSuffix = regexp.MustCompile(`-[0-9a-f]{7,40}$`)

// releaseSemver turns a build version into a comparable semver string. dev
// builds ("dev-<commit>") are not valid semver and report false.
func releaseSemver(v string) (string, bool) {
	v = "v" + strings.TrimPrefix(v, "v")
	v = releaseCommitSuffix.ReplaceAllString(v, "")
	if !semver.IsValid(v) {
		return "", false
	}
	return v, true
}

func startUpdateCheck() {
	pendingUpdateCheck = make(chan string, 1)
	go func(result chan<- string) {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		latest, err := fetchLatestVersion(ctx)
		if err != nil {
			latest = ""
		}
		result <- latest
	}(pendingUpdateCheck)
}

// waitForUpdateCheck records the background check once the command finishes.
// the fetch is bounded by updateCheckTimeout, so this adds at most that much
// latency, at most once a day. a failed check still counts as a check so an
// unreachable github is not retried on every run.
func waitForUpdateCheck() {
	if pendingUpdateCheck == nil {
		return
	}
	latest := <-pendingUpdateCheck
	pendingUpdateCheck = nil

	path := updateStatePath()
	state := loadUpdateState(path)
	state.CheckedAt = updateNow()
	if latest != "" {
		state.LatestVersion = latest
	}
	saveUpdateState(path, state)
}

func fetchLatestReleaseTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status: %s", resp.Status)
	}
	var release GithubApiResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if !semver.IsValid(release.Version) {
		return "", fmt.Errorf("invalid release tag %q", release.Version)
	}
	return release.Version, nil
}

func defaultUpdateStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".runpod", "update-check.json")
}

func loadUpdateState(path string) updateState {
	var state updateState
	if path == "" {
		return state
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}
	_ = json.Unmarshal(data, &state)
	return state
}

func saveUpdateState(path string, state updateState) {
	if path == "" {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// packageManagerUpdate returns the upgrade command for a binary installed by a
// package manager, or nil for a direct download. replacing a package-managed
// binary in place leaves the package manager tracking a version that is gone.
func packageManagerUpdate(exe string) []string {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	slashed := filepath.ToSlash(resolved)
	switch {
	case strings.Contains(slashed, "/Caskroom/"):
		return []string{"brew", "upgrade", "--cask", "runpodctl"}
	case strings.Contains(slashed, "/Cellar/"):
		return []string{"brew", "upgrade", "runpodctl"}
	case strings.Contains(slashed, "/.pixi/"):
		return []string{"pixi", "global", "update", "runpodctl"}
	}
	// conda puts the binary in <prefix>/bin, or <prefix>/Library/bin on windows
	dir := filepath.Dir(resolved)
	for _, prefix := range []string{filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		if info, err := os.Stat(filepath.Join(prefix, "conda-meta")); err == nil && info.IsDir() {
			return []string{"conda", "update", "-y", "-p", prefix, "runpodctl"}
		}
	}
	return nil
}

func installUpdateForChannel(out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find current executable: %w", err)
	}
	args := packageManagerUpdate(exe)
	if args == nil {
		return runSelfUpdate(out)
	}
	fmt.Fprintf(out, "running: %s\n", strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	// keep stdout clean for the command's own output
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w (run it manually: %s)", err, strings.Join(args, " "))
	}
	return nil
}
