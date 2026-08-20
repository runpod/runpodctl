package registry

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

// stubTerminal forces the prompt branch on and off regardless of how the test
// binary's stdin is wired, and restores the real detector afterwards.
func stubTerminal(t *testing.T, isTTY bool, secret string, promptErr error) *bool {
	t.Helper()
	origTTY, origPrompt := stdinIsTerminal, promptPassword
	t.Cleanup(func() { stdinIsTerminal, promptPassword = origTTY, origPrompt })

	prompted := false
	stdinIsTerminal = func() bool { return isTTY }
	promptPassword = func(io.Writer) (string, error) {
		prompted = true
		return secret, promptErr
	}
	return &prompted
}

func TestSignalExitCode(t *testing.T) {
	tests := []struct {
		name string
		sig  os.Signal
		want int
	}{
		{name: "interrupt", sig: os.Interrupt, want: 130},
		{name: "terminate", sig: syscall.SIGTERM, want: 143},
		{name: "quit", sig: syscall.SIGQUIT, want: 131},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := signalExitCode(tt.sig); got != tt.want {
				t.Errorf("signalExitCode(%v) = %d, want %d", tt.sig, got, tt.want)
			}
		})
	}
}

func TestResolvePasswordFromStdin(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		want  string
	}{
		{name: "plain value", stdin: "s3cret", want: "s3cret"},
		{name: "trailing newline from a pipe is stripped", stdin: "s3cret\n", want: "s3cret"},
		{name: "trailing crlf is stripped", stdin: "s3cret\r\n", want: "s3cret"},
		// a credential may legitimately contain these, so they must survive intact.
		{name: "inner whitespace survives", stdin: "s3 cret\n", want: "s3 cret"},
		{name: "leading whitespace survives", stdin: " s3cret\n", want: " s3cret"},
		{name: "trailing space before the newline survives", stdin: "s3cret \n", want: "s3cret "},
		{name: "a token that looks like a flag is not parsed", stdin: "--not-a-flag\n", want: "--not-a-flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubTerminal(t, false, "", nil)
			var stderr bytes.Buffer

			got, err := resolvePassword(strings.NewReader(tt.stdin), &stderr, "", true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("password = %q, want %q", got, tt.want)
			}
			// the secure path must not nag.
			if stderr.Len() != 0 {
				t.Errorf("expected no warning on the stdin path, got %q", stderr.String())
			}
		})
	}
}

// a gcr.io / artifact registry credential is an entire service-account json key
// passed as the password (username _json_key), the same shape docker login takes
// on stdin. it is multi-line, the api accepts it, and an earlier revision of this
// resolver rejected it -- so it stays pinned.
func TestResolvePasswordKeepsAMultiLineCredential(t *testing.T) {
	stubTerminal(t, false, "", nil)
	key := "{\n  \"type\": \"service_account\",\n  \"private_key\": \"-----BEGIN PRIVATE KEY-----\\nabc\\n-----END PRIVATE KEY-----\\n\"\n}"

	got, err := resolvePassword(strings.NewReader(key+"\n"), io.Discard, "", true)
	if err != nil {
		t.Fatalf("a multi-line credential must be accepted: %v", err)
	}
	if got != key {
		t.Errorf("password = %q, want the key back with only the trailing newline stripped", got)
	}
}

func TestResolvePasswordRejectsBadStdin(t *testing.T) {
	tests := []struct {
		name    string
		stdin   string
		wantMsg string
	}{
		{name: "empty stdin", stdin: "", wantMsg: "empty"},
		{name: "only a newline", stdin: "\n", wantMsg: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubTerminal(t, false, "", nil)

			_, err := resolvePassword(strings.NewReader(tt.stdin), io.Discard, "", true)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tt.wantMsg)
			}
			assertUsageError(t, err)
		})
	}
}

func TestResolvePasswordFromStdinReportsReadFailure(t *testing.T) {
	stubTerminal(t, false, "", nil)
	sentinel := errors.New("pipe broke")

	_, err := resolvePassword(errReader{sentinel}, io.Discard, "", true)
	if err == nil {
		t.Fatal("expected an error")
	}
	// an unreadable pipe is an environment failure, not a caller mistake, so it
	// must not be coded as a usage error.
	if !errors.Is(err, sentinel) {
		t.Errorf("expected the cause to stay wrapped, got %v", err)
	}
	var coder interface{ ErrorCode() string }
	if errors.As(err, &coder) {
		t.Errorf("a broken pipe must not be a %s", coder.ErrorCode())
	}
}

func TestResolvePasswordFromFlag(t *testing.T) {
	stubTerminal(t, false, "", nil)
	var stderr bytes.Buffer

	got, err := resolvePassword(strings.NewReader(""), &stderr, "s3cret", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "s3cret" {
		t.Errorf("password = %q, want s3cret", got)
	}
	// --password is a supported choice, not a mistake: it must not nag, and it
	// must never echo the credential it was handed.
	if stderr.Len() != 0 {
		t.Errorf("expected no output on the --password path, got %q", stderr.String())
	}
}

// --password wins over an unread stdin: cobra rejects the two flags together, so
// this only fixes the precedence for a caller that pipes without asking for it.
func TestResolvePasswordFlagDoesNotConsumeStdin(t *testing.T) {
	stubTerminal(t, false, "", nil)
	stdin := strings.NewReader("from-stdin\n")

	got, err := resolvePassword(stdin, io.Discard, "from-flag", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from-flag" {
		t.Errorf("password = %q, want from-flag", got)
	}
	if left, _ := io.ReadAll(stdin); string(left) != "from-stdin\n" {
		t.Errorf("stdin must be left untouched, %q was consumed", "from-stdin\n")
	}
}

func TestResolvePasswordPromptsOnATerminal(t *testing.T) {
	prompted := stubTerminal(t, true, "typed-secret", nil)

	got, err := resolvePassword(strings.NewReader(""), io.Discard, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*prompted {
		t.Error("expected the terminal to be prompted")
	}
	if got != "typed-secret" {
		t.Errorf("password = %q, want typed-secret", got)
	}
}

func TestResolvePasswordRejectsAnEmptyPrompt(t *testing.T) {
	stubTerminal(t, true, "", nil)

	_, err := resolvePassword(strings.NewReader(""), io.Discard, "", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	assertUsageError(t, err)
}

func TestResolvePasswordPropagatesPromptFailure(t *testing.T) {
	sentinel := errors.New("no tty after all")
	stubTerminal(t, true, "", sentinel)

	_, err := resolvePassword(strings.NewReader(""), io.Discard, "", false)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected the prompt failure to propagate, got %v", err)
	}
}

// the regression that matters for scripts: no password source and no terminal
// must fail fast rather than block forever on a read that will never arrive.
func TestResolvePasswordFailsWithoutATerminalOrASource(t *testing.T) {
	prompted := stubTerminal(t, false, "unused", nil)

	_, err := resolvePassword(strings.NewReader(""), io.Discard, "", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if *prompted {
		t.Error("must not prompt when stdin is not a terminal")
	}
	if !strings.Contains(err.Error(), "--password-stdin") {
		t.Errorf("error must name the safe flag, got %q", err.Error())
	}
	assertUsageError(t, err)
}

func assertUsageError(t *testing.T, err error) {
	t.Helper()
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) {
		t.Fatalf("expected a coded error, got %v", err)
	}
	if coder.ErrorCode() != "usage_error" {
		t.Errorf("code = %q, want usage_error", coder.ErrorCode())
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
