package cmd

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

func TestHelpTextConsistency(t *testing.T) {
	root := GetRootCmd()
	visited := map[*cobra.Command]bool{}

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd == nil || visited[cmd] {
			return
		}
		visited[cmd] = true

		checkHelpText(t, cmd.Use, cmd.Short, "short")
		checkHelpText(t, cmd.Use, cmd.Long, "long")

		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}

	walk(root)
}

func checkHelpText(t *testing.T, use, text, field string) {
	t.Helper()

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	if strings.Contains(trimmed, "(s)") {
		t.Errorf("%s %s contains '(s)': %q", use, field, trimmed)
		return
	}
	r, _ := utf8.DecodeRuneInString(trimmed)
	if unicode.IsLetter(r) && !unicode.IsLower(r) {
		t.Errorf("%s %s should start lowercase: %q", use, field, trimmed)
	}
}
