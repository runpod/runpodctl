package main

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// baselineHeader is written at the top of every baseline file.
const baselineHeader = `# nix/checks/lint-baseline.txt — runpodctl
#
# The lint ratchet: every finding that existed when a tool was turned on.
# One line per finding, "<tool>\t<path> | <message> (<rule>)". Line and column
# are deliberately absent, and digits in messages read as N, so a finding an
# unrelated edit merely moved does not read as new.
#
# Checked by the lint-baseline flake check: a finding not listed here fails it.
# A listed finding that no longer exists is only reported; the nix-maintenance
# pr prunes it, so the file only shrinks. Never edit by hand; to prune, run:
#
#   nix run .#update-lint-baseline
#
# Lines are sorted; a line repeats once per identical finding in the same file.
`

// Baseline maps tool -> finding key -> how many times it occurs.
type Baseline map[string]map[string]int

// countKeys builds the per-key multiset for one tool's findings.
func countKeys(findings []Finding) map[string]int {
	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Key()]++
	}
	return counts
}

// parseBaseline reads a baseline file. Lines must be sorted, so the file has
// one canonical form and a hand edit or a bad merge is caught, not absorbed.
func parseBaseline(r io.Reader) (Baseline, error) {
	b := Baseline{}
	var prev string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tool, key, ok := strings.Cut(line, "\t")
		if !ok || tool == "" || key == "" {
			return nil, fmt.Errorf("line %d: want \"<tool>\\t<key>\", got %q", n, line)
		}
		if line < prev {
			return nil, fmt.Errorf("line %d: not sorted (regenerate with update-lint-baseline)", n)
		}
		prev = line
		if b[tool] == nil {
			b[tool] = map[string]int{}
		}
		b[tool][key]++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	return b, nil
}

// writeBaseline renders the canonical baseline for the measured tools.
func writeBaseline(w io.Writer, measured map[string][]Finding) error {
	var lines []string
	for tool, findings := range measured {
		for key, count := range countKeys(findings) {
			for range count {
				lines = append(lines, tool+"\t"+key)
			}
		}
	}
	slices.Sort(lines)
	bw := bufio.NewWriter(w)
	if _, err := bw.WriteString(baselineHeader); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := bw.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// Delta is how one tool's findings differ from its baseline.
type Delta struct {
	// New are keys measured more often than the baseline allows.
	New []string
	// Fixed are keys the baseline lists more often than they now occur.
	Fixed []string
}

// compare diffs measured findings against the baseline as multisets.
func compare(baseline map[string]int, findings []Finding) Delta {
	measured := countKeys(findings)
	var d Delta
	for _, key := range slices.Sorted(maps.Keys(measured)) {
		for range measured[key] - baseline[key] {
			d.New = append(d.New, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(baseline)) {
		for range baseline[key] - measured[key] {
			d.Fixed = append(d.Fixed, key)
		}
	}
	return d
}
