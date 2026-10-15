// Command staticreport turns the static analysis tools' JSON output into the
// lint ratchet and a human-readable report. The Nix flake runs every tool once
// (nix/static-analysis/measure.nix) and feeds the results here:
//
//	staticreport baseline -baseline FILE -root DIR -in NAME=FORMAT:PATH... -exit NAME=N... [-update]
//	staticreport report   -baseline FILE -root DIR -in NAME=FORMAT:PATH... -exit NAME=N... \
//	                      [-vuln PATH] [-meta-file PATH] -md OUT.md -json OUT.json
//
// FORMAT is one of golangci, staticcheck or gosec. `baseline` exits 0 when the
// findings match the baseline exactly, 1 when a finding is new or a listed one
// is gone (the ratchet only moves by update-lint-baseline), 2 on unusable
// input, and 3 when a tool failed rather than reporting findings.
package main

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	exitOK       = 0
	exitFindings = 1
	exitUsage    = 2
	exitToolFail = 3
)

func main() {
	// Output is buffered and written once, so every write error is seen.
	var stdout, stderr bytes.Buffer
	code := run(os.Args[1:], &stdout, &stderr)
	if _, err := os.Stdout.Write(stdout.Bytes()); err != nil {
		code = cmp.Or(code, exitUsage)
	}
	if _, err := os.Stderr.Write(stderr.Bytes()); err != nil {
		code = cmp.Or(code, exitUsage)
	}
	os.Exit(code)
}

// pairs is a repeatable NAME=VALUE flag, kept in command-line order.
type pairs [][2]string

func (p *pairs) String() string { return fmt.Sprint(*p) }

func (p *pairs) Set(s string) error {
	name, value, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return fmt.Errorf("want NAME=VALUE, got %q", s)
	}
	*p = append(*p, [2]string{name, value})
	return nil
}

// options are the flags shared by both subcommands.
type options struct {
	baseline string
	root     string
	inputs   pairs
	exits    pairs
	update   bool
	vuln     string
	metaFile string
	mdOut    string
	jsonOut  string
	maxList  int
}

// Measured is one tool's run: its findings and whether it ran to completion.
type Measured struct {
	Name     string
	Format   string
	Exit     int
	Failed   bool
	Findings []Finding
}

func run(args []string, stdout, stderr *bytes.Buffer) int {
	if len(args) == 0 || (args[0] != "baseline" && args[0] != "report") {
		fmt.Fprintln(stderr, "usage: staticreport baseline|report [flags]")
		return exitUsage
	}
	cmd := args[0]
	fs := flag.NewFlagSet("staticreport "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.baseline, "baseline", "", "baseline file")
	fs.StringVar(&o.root, "root", ".", "repo root the tools ran in, to make paths relative")
	fs.Var(&o.inputs, "in", "NAME=FORMAT:PATH of a tool's json output (repeatable)")
	fs.Var(&o.exits, "exit", "NAME=STATUS the tool exited with (repeatable)")
	fs.BoolVar(&o.update, "update", false, "baseline: rewrite the baseline from the measured findings")
	fs.StringVar(&o.vuln, "vuln", "", "report: govulncheck -format json output")
	fs.StringVar(&o.metaFile, "meta-file", "", "report: file of KEY=VALUE provenance lines")
	fs.StringVar(&o.mdOut, "md", "", "report: markdown output path")
	fs.StringVar(&o.jsonOut, "json", "", "report: json output path")
	fs.IntVar(&o.maxList, "max-list", 300, "report: findings listed per tool")
	if err := fs.Parse(args[1:]); err != nil {
		return exitUsage
	}

	measured, err := load(&o)
	if err != nil {
		fmt.Fprintln(stderr, "staticreport:", err)
		return exitUsage
	}
	baseline, err := readBaseline(o.baseline, o.update)
	if err != nil {
		fmt.Fprintln(stderr, "staticreport:", err)
		return exitUsage
	}

	if cmd == "report" {
		return runReport(&o, measured, baseline, stdout, stderr)
	}
	return runBaseline(&o, measured, baseline, stdout, stderr)
}

// load reads every -in file and pairs it with its -exit status.
func load(o *options) ([]Measured, error) {
	if len(o.inputs) == 0 {
		return nil, errors.New("no -in inputs")
	}
	exits := map[string]int{}
	for _, e := range o.exits {
		n, err := strconv.Atoi(e[1])
		if err != nil {
			return nil, fmt.Errorf("-exit %s: %w", e[0], err)
		}
		exits[e[0]] = n
	}
	var out []Measured
	for _, in := range o.inputs {
		name := in[0]
		format, path, ok := strings.Cut(in[1], ":")
		if !ok {
			return nil, fmt.Errorf("-in %s: want FORMAT:PATH", name)
		}
		status, ok := exits[name]
		if !ok {
			return nil, fmt.Errorf("-in %s has no -exit %s=N", name, name)
		}
		m := Measured{Name: name, Format: format, Exit: status, Failed: status > maxExit[format]}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("-in %s: %w", name, err)
		}
		m.Findings, err = decodeFile(format, data, o.root)
		if err != nil {
			if !m.Failed {
				return nil, fmt.Errorf("-in %s: %w", name, err)
			}
			// A failed tool's output is expected to be unusable; report the
			// failure rather than the decode error.
			m.Findings = nil
		}
		// For these tools exit 1 means "found something", so exit 1 with
		// nothing found is a failure whose output went elsewhere: without
		// this, a lost report reads as a clean run.
		if status == 1 && len(m.Findings) == 0 && (format == formatGolangci || format == formatStaticcheck) {
			m.Failed = true
		}
		for i := range m.Findings {
			m.Findings[i].Tool = name
		}
		out = append(out, m)
	}
	return out, nil
}

// readBaseline loads the baseline. -update replaces the file wholesale, so it
// never reads the old one (which may be the malformed file being repaired).
func readBaseline(path string, update bool) (Baseline, error) {
	if path == "" {
		return nil, errors.New("-baseline is required")
	}
	if update {
		return Baseline{}, nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	return parseBaseline(bytes.NewReader(data))
}

func runBaseline(o *options, measured []Measured, baseline Baseline, stdout, stderr *bytes.Buffer) int {
	for _, m := range measured {
		if m.Failed {
			fmt.Fprintf(stderr, "%s exited %d: the tool failed, so its findings are incomplete (see its log)\n", m.Name, m.Exit)
			return exitToolFail
		}
	}

	if o.update {
		byTool := map[string][]Finding{}
		for _, m := range measured {
			byTool[m.Name] = m.Findings
		}
		var buf bytes.Buffer
		err := writeBaseline(&buf, byTool)
		if err == nil {
			err = os.WriteFile(filepath.Clean(o.baseline), buf.Bytes(), 0o600)
		}
		if err != nil {
			fmt.Fprintln(stderr, "staticreport:", err)
			return exitUsage
		}
		fmt.Fprintf(stdout, "wrote %s\n", o.baseline)
		return exitOK
	}

	rc := exitOK
	for _, m := range measured {
		d := compare(baseline[m.Name], m.Findings)
		fmt.Fprintf(stdout, "%-16s %5d findings, %d new, %d fixed\n", m.Name, len(m.Findings), len(d.New), len(d.Fixed))
		for _, key := range d.New {
			fmt.Fprintf(stdout, "  new:   %s\n", key)
		}
		for _, key := range d.Fixed {
			fmt.Fprintf(stdout, "  fixed: %s\n", key)
		}
		if len(d.New) > 0 || len(d.Fixed) > 0 {
			rc = exitFindings
		}
	}
	for tool := range baseline {
		if !slices.ContainsFunc(measured, func(m Measured) bool { return m.Name == tool }) {
			fmt.Fprintf(stderr, "baseline lists %s, which was not measured\n", tool)
			return exitUsage
		}
	}
	switch rc {
	case exitOK:
		fmt.Fprintln(stdout, "lint-baseline: findings match the baseline")
	default:
		fmt.Fprintln(stdout, "lint-baseline: fix the new findings; if findings were fixed, run `nix run .#update-lint-baseline` and commit the shrunk baseline")
	}
	return rc
}
