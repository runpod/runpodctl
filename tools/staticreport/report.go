package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ToolSummary is one tool's row in the report.
type ToolSummary struct {
	Name      string         `json:"name"`
	Exit      int            `json:"exit"`
	Failed    bool           `json:"failed"`
	Findings  int            `json:"findings"`
	New       int            `json:"new"`
	Fixed     int            `json:"fixed"`
	ByRule    map[string]int `json:"byRule"`
	ByPackage map[string]int `json:"byPackage"`
}

// Report is the machine-readable form of the report (report.json).
type Report struct {
	Meta  map[string]string `json:"meta"`
	Tools []ToolSummary     `json:"tools"`
	// VulnScanned separates "govulncheck found nothing" from "not run".
	VulnScanned bool      `json:"vulnScanned"`
	Vulns       []Vuln    `json:"vulns"`
	NewItems    []Finding `json:"new,omitempty"`
}

func runReport(o *options, measured []Measured, baseline Baseline, stdout, stderr *bytes.Buffer) int {
	var vulns []Vuln
	if o.vuln != "" {
		data, err := os.ReadFile(filepath.Clean(o.vuln))
		if err == nil {
			vulns, err = decodeGovulncheck(bytes.NewReader(data))
		}
		if err != nil {
			fmt.Fprintln(stderr, "staticreport:", err)
			return exitUsage
		}
	}

	meta, err := readMeta(o.metaFile)
	if err != nil {
		fmt.Fprintln(stderr, "staticreport:", err)
		return exitUsage
	}
	rep := buildReport(meta, measured, baseline, vulns)
	rep.VulnScanned = o.vuln != ""
	md := renderMarkdown(&rep, measured, o.maxList)

	if err := writeOut(o.mdOut, stdout, []byte(md)); err != nil {
		fmt.Fprintln(stderr, "staticreport:", err)
		return exitUsage
	}
	if o.jsonOut != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "staticreport:", err)
			return exitUsage
		}
		if err := os.WriteFile(filepath.Clean(o.jsonOut), append(data, '\n'), 0o600); err != nil {
			fmt.Fprintln(stderr, "staticreport:", err)
			return exitUsage
		}
	}
	return exitOK
}

func writeOut(path string, stdout *bytes.Buffer, data []byte) error {
	if path == "" || path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	return os.WriteFile(filepath.Clean(path), data, 0o600)
}

// readMeta reads KEY=VALUE provenance lines (tool versions, the commit).
func readMeta(path string) (map[string]string, error) {
	meta := map[string]string{}
	if path == "" {
		return meta, nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%s: want KEY=VALUE, got %q", path, line)
		}
		meta[key] = value
	}
	return meta, nil
}

func buildReport(meta map[string]string, measured []Measured, baseline Baseline, vulns []Vuln) Report {
	rep := Report{Meta: meta, Vulns: vulns}
	for _, m := range measured {
		d := compare(baseline[m.Name], m.Findings)
		s := ToolSummary{
			Name:      m.Name,
			Exit:      m.Exit,
			Failed:    m.Failed,
			Findings:  len(m.Findings),
			New:       len(d.New),
			Fixed:     len(d.Fixed),
			ByRule:    map[string]int{},
			ByPackage: map[string]int{},
		}
		newKeys := map[string]int{}
		for _, key := range d.New {
			newKeys[key]++
		}
		for _, f := range m.Findings {
			s.ByRule[f.Rule]++
			s.ByPackage[f.Package()]++
			if newKeys[f.Key()] > 0 {
				newKeys[f.Key()]--
				rep.NewItems = append(rep.NewItems, f)
			}
		}
		rep.Tools = append(rep.Tools, s)
	}
	return rep
}

// ranked returns a count map's entries, largest first, then by name.
func ranked(counts map[string]int) []string {
	return slices.SortedFunc(maps.Keys(counts), func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
}

// cell escapes text for a markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.ReplaceAll(s, "\n", " ")
}

func renderMarkdown(rep *Report, measured []Measured, maxList int) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# runpodctl static analysis report\n\n")
	if len(rep.Meta) > 0 {
		w("| | |\n| --- | --- |\n")
		for _, k := range slices.Sorted(maps.Keys(rep.Meta)) {
			w("| %s | `%s` |\n", cell(k), cell(rep.Meta[k]))
		}
		w("\n")
	}

	w("## Summary\n\n")
	w("| Tool | Exit | Findings | New vs baseline | Fixed vs baseline |\n| --- | ---: | ---: | ---: | ---: |\n")
	total := 0
	for _, s := range rep.Tools {
		status := strconv.Itoa(s.Exit)
		if s.Failed {
			status += " ❌ tool failed"
		}
		w("| %s | %s | %d | %d | %d |\n", s.Name, status, s.Findings, s.New, s.Fixed)
		total += s.Findings
	}
	w("\n%d findings in total. Tiers overlap (each golangci-lint tier is a superset of the one before), so totals across tools double-count.\n\n", total)

	renderSecurity(&b, rep, measured)

	w("## Findings by package\n\n")
	pkgs := map[string]bool{}
	for _, s := range rep.Tools {
		for p := range s.ByPackage {
			pkgs[p] = true
		}
	}
	w("| Package |")
	for _, s := range rep.Tools {
		w(" %s |", s.Name)
	}
	w("\n| --- |%s\n", strings.Repeat(" ---: |", len(rep.Tools)))
	for _, p := range slices.Sorted(maps.Keys(pkgs)) {
		w("| `%s` |", p)
		for _, s := range rep.Tools {
			w(" %d |", s.ByPackage[p])
		}
		w("\n")
	}
	w("\n")

	if len(rep.NewItems) > 0 {
		w("## New findings (not in the baseline)\n\n")
		for _, f := range rep.NewItems {
			w("- `%s:%d` %s (%s, %s)\n", f.Path, f.Line, cell(f.Message), f.Tool, f.Rule)
		}
		w("\n")
	}

	w("## Findings by tool\n\n")
	for i, s := range rep.Tools {
		w("### %s\n\n", s.Name)
		if s.Failed {
			w("> **The tool failed (exit %d)**, so these results are incomplete. See `raw/%s.log`.\n\n", s.Exit, s.Name)
		}
		if s.Findings == 0 {
			w("No findings.\n\n")
			continue
		}
		w("| Rule | Count |\n| --- | ---: |\n")
		for _, r := range ranked(s.ByRule) {
			w("| %s | %d |\n", cell(r), s.ByRule[r])
		}
		w("\n")
		findings := slices.SortedFunc(slices.Values(measured[i].Findings), func(a, b Finding) int {
			return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
		})
		w("<details><summary>%d findings</summary>\n\n", len(findings))
		for n, f := range findings {
			if n == maxList {
				w("- … %d more in `report.json` / `raw/`\n", len(findings)-maxList)
				break
			}
			w("- `%s:%d:%d` %s (%s)\n", f.Path, f.Line, f.Column, cell(f.Message), f.Rule)
		}
		w("\n</details>\n\n")
	}
	return b.String()
}

// renderSecurity collects what a security review reads first: gosec by rule
// and severity, staticcheck's bug-finding SA checks, and govulncheck.
func renderSecurity(b *strings.Builder, rep *Report, measured []Measured) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	w("## Security\n\n")

	renderVulns(b, rep)

	for _, m := range measured {
		switch m.Format {
		case formatGosec:
			bySev := map[string]int{}
			for _, f := range m.Findings {
				bySev[f.Rule+" "+f.Severity]++
			}
			w("### gosec by rule and severity/confidence\n\n")
			if len(bySev) == 0 {
				w("No findings.\n\n")
				continue
			}
			w("| Rule severity/confidence | Count |\n| --- | ---: |\n")
			for _, k := range ranked(bySev) {
				w("| %s | %d |\n", k, bySev[k])
			}
			w("\n")
		case formatStaticcheck:
			sa := map[string]int{}
			for _, f := range m.Findings {
				if strings.HasPrefix(f.Rule, "SA") {
					sa[f.Rule]++
				}
			}
			w("### staticcheck SA (likely bugs)\n\n")
			if len(sa) == 0 {
				w("No SA findings.\n\n")
				continue
			}
			w("| Check | Count |\n| --- | ---: |\n")
			for _, k := range ranked(sa) {
				w("| %s | %d |\n", k, sa[k])
			}
			w("\n")
		}
	}
}

// renderVulns lists govulncheck's advisories, reachable ones first.
func renderVulns(b *strings.Builder, rep *Report) {
	w := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	called := 0
	for _, v := range rep.Vulns {
		if v.Level == levelCalled {
			called++
		}
	}
	w("### govulncheck\n\n")
	switch {
	case !rep.VulnScanned:
		w("Not run (it needs the live vulnerability database). Use `nix run .#static-analysis-report-full`.\n\n")
	case len(rep.Vulns) == 0:
		w("No known vulnerabilities.\n\n")
	default:
		w("%d advisories, **%d reachable** from runpodctl's code.\n\n", len(rep.Vulns), called)
		w("| Advisory | Level | Module | Found | Fixed | Summary |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, v := range slices.SortedFunc(slices.Values(rep.Vulns), func(a, b Vuln) int {
			return cmp.Or(cmp.Compare(levelRank(b.Level), levelRank(a.Level)), cmp.Compare(a.ID, b.ID))
		}) {
			w("| %s | %s | `%s` | %s | %s | %s |\n", v.ID, v.Level, v.Module, v.Found, v.Fixed, cell(v.Summary))
		}
		w("\n")
	}
}

func levelRank(level string) int {
	switch level {
	case levelCalled:
		return 2
	case levelImported:
		return 1
	default:
		return 0
	}
}
