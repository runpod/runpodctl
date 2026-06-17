// Package agent detects which AI coding agent (if any) is driving the CLI.
//
// Detection is based on the environment variables that agent harnesses set in
// the processes they spawn. The registry mirrors Hugging Face's public
// agent-harnesses list so that traffic is attributed under the same agent
// identifiers across tools:
// https://github.com/huggingface/huggingface.js/blob/main/packages/tasks/src/agent-harnesses.ts
package agent

import (
	"os"
	"strings"
)

// harness maps an agent identifier to the environment variables that identify
// it. Detection matches if ANY of the listed variables is set to a non-empty
// value.
type harness struct {
	id      string
	envVars []string
}

// harnesses is checked in order; the first match wins. Order matters: more
// specific signals must come before broader ones that they can co-occur with
// (e.g. cowork before claude-code, cursor-cli before cursor).
var harnesses = []harness{
	{id: "antigravity", envVars: []string{"ANTIGRAVITY_AGENT"}},
	{id: "augment-cli", envVars: []string{"AUGMENT_AGENT"}},
	{id: "cline", envVars: []string{"CLINE_ACTIVE"}},
	{id: "cowork", envVars: []string{"CLAUDE_CODE_IS_COWORK"}},
	{id: "claude-code", envVars: []string{"CLAUDECODE", "CLAUDE_CODE"}},
	{id: "codex", envVars: []string{"CODEX_SANDBOX", "CODEX_CI", "CODEX_THREAD_ID"}},
	{id: "crush", envVars: []string{"CRUSH"}},
	{id: "gemini-cli", envVars: []string{"GEMINI_CLI"}},
	{id: "github-copilot", envVars: []string{"COPILOT_MODEL", "COPILOT_ALLOW_ALL", "COPILOT_GITHUB_TOKEN"}},
	{id: "goose", envVars: []string{"GOOSE_TERMINAL"}},
	{id: "hermes-agent", envVars: []string{"HERMES_SESSION_ID"}},
	{id: "kilo-code", envVars: []string{"KILOCODE_FEATURE"}},
	{id: "kiro", envVars: []string{"AGENT_CONTEXT_OUT"}},
	{id: "openclaw", envVars: []string{"OPENCLAW_SHELL"}},
	{id: "opencode", envVars: []string{"OPENCODE_CLIENT"}},
	{id: "pi", envVars: []string{"PI_CODING_AGENT"}},
	{id: "replit", envVars: []string{"REPL_ID"}},
	{id: "trae", envVars: []string{"TRAE_AI_SHELL_ID"}},
	{id: "zed", envVars: []string{"ZED_TERM"}},
	{id: "cursor-cli", envVars: []string{"CURSOR_AGENT"}},
	{id: "cursor", envVars: []string{"CURSOR_TRACE_ID"}},
}

// standardEnvVars are generic variables any tool can set to identify itself.
// When set, the value is sanitized and used as the agent id. Only AI_AGENT is
// honored: a bare AGENT is too common in unrelated environments (CI runners,
// shell setups) and would attribute traffic to arbitrary values.
var standardEnvVars = []string{"AI_AGENT"}

// KnownEnvVars returns every environment variable the registry inspects,
// including the standard AI_AGENT signal. Useful for tests that need to
// isolate detection from the ambient environment.
func KnownEnvVars() []string {
	var vars []string
	for _, h := range harnesses {
		vars = append(vars, h.envVars...)
	}
	return append(vars, standardEnvVars...)
}

// Suffix returns the " (via <id>)" User-Agent fragment for the detected agent,
// or an empty string when none is detected. Centralizing the fragment here
// keeps the tag format identical across every client's User-Agent.
func Suffix() string {
	if a := Detect(); a != "" {
		return " (via " + a + ")"
	}
	return ""
}

// Detect returns the identifier of the AI coding agent driving the CLI, or an
// empty string if none is detected. Specific harness markers take priority
// over the generic AI_AGENT signal.
func Detect() string {
	for _, h := range harnesses {
		for _, env := range h.envVars {
			if os.Getenv(env) != "" {
				return h.id
			}
		}
	}
	for _, env := range standardEnvVars {
		if v := sanitize(os.Getenv(env)); v != "" {
			return v
		}
	}
	return ""
}

// sanitize trims the value and keeps only User-Agent-safe characters
// ([A-Za-z0-9._-]), capped at 64 runes, so an arbitrary env value can't
// produce a malformed header.
func sanitize(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	for _, r := range v {
		if b.Len() >= 64 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}
