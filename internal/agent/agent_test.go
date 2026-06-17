package agent

import (
	"strings"
	"testing"
)

func clearAll(t *testing.T) {
	t.Helper()
	for _, env := range KnownEnvVars() {
		t.Setenv(env, "")
	}
}

func TestDetect_None(t *testing.T) {
	clearAll(t)
	if got := Detect(); got != "" {
		t.Errorf("expected no agent, got %q", got)
	}
}

func TestDetect_KnownHarnesses(t *testing.T) {
	cases := map[string]string{
		"CLAUDECODE":        "claude-code",
		"CLAUDE_CODE":       "claude-code",
		"CODEX_SANDBOX":     "codex",
		"CODEX_THREAD_ID":   "codex",
		"GEMINI_CLI":        "gemini-cli",
		"CURSOR_AGENT":      "cursor-cli",
		"CURSOR_TRACE_ID":   "cursor",
		"OPENCODE_CLIENT":   "opencode",
		"COPILOT_MODEL":     "github-copilot",
		"ANTIGRAVITY_AGENT": "antigravity",
	}
	for env, want := range cases {
		t.Run(env, func(t *testing.T) {
			clearAll(t)
			t.Setenv(env, "1")
			if got := Detect(); got != want {
				t.Errorf("env %s: expected %q, got %q", env, want, got)
			}
		})
	}
}

func TestDetect_CoworkBeatsClaudeCode(t *testing.T) {
	clearAll(t)
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_IS_COWORK", "1")
	if got := Detect(); got != "cowork" {
		t.Errorf("expected cowork to win, got %q", got)
	}
}

func TestDetect_CursorCLIBeatsCursor(t *testing.T) {
	clearAll(t)
	t.Setenv("CURSOR_TRACE_ID", "abc")
	t.Setenv("CURSOR_AGENT", "1")
	if got := Detect(); got != "cursor-cli" {
		t.Errorf("expected cursor-cli to win, got %q", got)
	}
}

func TestDetect_StandardFallback(t *testing.T) {
	clearAll(t)
	t.Setenv("AI_AGENT", "my-harness")
	if got := Detect(); got != "my-harness" {
		t.Errorf("expected my-harness, got %q", got)
	}
}

func TestDetect_SpecificBeatsStandard(t *testing.T) {
	clearAll(t)
	t.Setenv("AI_AGENT", "my-harness")
	t.Setenv("CLAUDECODE", "1")
	if got := Detect(); got != "claude-code" {
		t.Errorf("expected claude-code to take priority, got %q", got)
	}
}

func TestDetect_BareAgentNotHonored(t *testing.T) {
	clearAll(t)
	t.Setenv("AGENT", "ssh-agent-or-ci-runner")
	if got := Detect(); got != "" {
		t.Errorf("expected bare AGENT to be ignored, got %q", got)
	}
}

func TestDetect_SanitizesStandardValue(t *testing.T) {
	cases := map[string]string{
		"my harness\r\nX-Injected: 1": "myharnessX-Injected1",
		"  trimmed-agent  ":           "trimmed-agent",
		"!@#$%":                       "",
	}
	for in, want := range cases {
		t.Run(want, func(t *testing.T) {
			clearAll(t)
			t.Setenv("AI_AGENT", in)
			if got := Detect(); got != want {
				t.Errorf("AI_AGENT=%q: expected %q, got %q", in, want, got)
			}
		})
	}
}

func TestSuffix(t *testing.T) {
	clearAll(t)
	if got := Suffix(); got != "" {
		t.Errorf("expected empty suffix, got %q", got)
	}
	t.Setenv("CLAUDECODE", "1")
	if got := Suffix(); got != " (via claude-code)" {
		t.Errorf("expected \" (via claude-code)\", got %q", got)
	}
}

func TestSanitize_CapsLength(t *testing.T) {
	if got := sanitize(strings.Repeat("a", 100)); len(got) != 64 {
		t.Errorf("expected 64-rune cap, got %d", len(got))
	}
}
