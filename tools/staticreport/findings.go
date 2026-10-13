package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Finding is one static analysis result, normalized across tools.
type Finding struct {
	Tool     string `json:"tool"`
	Rule     string `json:"rule"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
}

// digits is stripped from baseline keys: line counts and complexity scores
// in messages (nakedret, gocognit, dupl) change on unrelated edits, and a
// moved finding must not read as a new one.
var digits = regexp.MustCompile(`\d+`)

// Key identifies a finding independently of its line and column, so editing
// a file above a finding doesn't make it look new. Whitespace runs collapse
// to one space: some messages quote multi-line source, and a key is one line
// of the baseline file.
func (f *Finding) Key() string {
	msg := strings.Join(strings.Fields(f.Message), " ")
	return f.Path + " | " + digits.ReplaceAllString(msg, "N") + " (" + f.Rule + ")"
}

// Package is the directory a finding is in, which for Go is its package.
func (f *Finding) Package() string {
	dir := filepath.ToSlash(filepath.Dir(f.Path))
	if dir == "." {
		return "(root)"
	}
	return dir
}

// Vuln is one govulncheck advisory affecting the module.
type Vuln struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases,omitempty"`
	Summary string   `json:"summary"`
	Module  string   `json:"module"`
	Found   string   `json:"found"`
	Fixed   string   `json:"fixed,omitempty"`
	// Level is how deep the evidence goes: "called" (a vulnerable symbol is
	// reachable), "imported" (a vulnerable package is imported) or
	// "required" (only the module is in the build list).
	Level string `json:"level"`
}

// Input formats, and govulncheck's evidence levels.
const (
	formatGolangci    = "golangci"
	formatStaticcheck = "staticcheck"
	formatGosec       = "gosec"
	formatGovulncheck = "govulncheck"

	levelRequired = "required"
	levelImported = "imported"
	levelCalled   = "called"
)

// decoder reads one tool's JSON output.
type decoder func(r io.Reader, root string) ([]Finding, error)

var decoders = map[string]decoder{
	formatGolangci:    decodeGolangci,
	formatStaticcheck: decodeStaticcheck,
	formatGosec:       decodeGosec,
}

// maxExit is the highest exit status that still means "the tool ran". Above
// it the tool failed (a crash, timeout or load error) and its output can't
// be trusted to be complete.
var maxExit = map[string]int{
	formatGolangci:    1,
	formatStaticcheck: 1,
	formatGosec:       1,
	formatGovulncheck: 3,
}

// relPath turns a tool's path into one relative to the repo root, so the
// same finding has the same key inside and outside the sandbox.
func relPath(path, root string) string {
	if root != "" && filepath.IsAbs(path) {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func decodeGolangci(r io.Reader, root string) ([]Finding, error) {
	var doc struct {
		Issues []struct {
			FromLinter string
			Text       string
			Severity   string
			Pos        struct {
				Filename string
				Line     int
				Column   int
			}
		}
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("golangci-lint json: %w", err)
	}
	out := make([]Finding, 0, len(doc.Issues))
	for _, issue := range doc.Issues {
		out = append(out, Finding{
			Tool:     formatGolangci,
			Rule:     issue.FromLinter,
			Path:     relPath(issue.Pos.Filename, root),
			Line:     issue.Pos.Line,
			Column:   issue.Pos.Column,
			Message:  issue.Text,
			Severity: issue.Severity,
		})
	}
	return out, nil
}

// decodeStaticcheck reads `staticcheck -f json`: one object per line.
func decodeStaticcheck(r io.Reader, root string) ([]Finding, error) {
	var out []Finding
	dec := json.NewDecoder(r)
	for {
		var issue struct {
			Code     string
			Severity string
			Message  string
			Location struct {
				File   string
				Line   int
				Column int
			}
		}
		err := dec.Decode(&issue)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("staticcheck json: %w", err)
		}
		out = append(out, Finding{
			Tool:     formatStaticcheck,
			Rule:     issue.Code,
			Path:     relPath(issue.Location.File, root),
			Line:     issue.Location.Line,
			Column:   issue.Location.Column,
			Message:  issue.Message,
			Severity: issue.Severity,
		})
	}
}

func decodeGosec(r io.Reader, root string) ([]Finding, error) {
	var doc struct {
		GolangErrors map[string]json.RawMessage `json:"Golang errors"`
		Issues       []struct {
			Severity   string `json:"severity"`
			Confidence string `json:"confidence"`
			RuleID     string `json:"rule_id"`
			Details    string `json:"details"`
			File       string `json:"file"`
			Line       string `json:"line"`
			Column     string `json:"column"`
		}
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("gosec json: %w", err)
	}
	if len(doc.GolangErrors) > 0 {
		return nil, fmt.Errorf("gosec could not load %d file(s); its results are incomplete", len(doc.GolangErrors))
	}
	out := make([]Finding, 0, len(doc.Issues))
	for _, issue := range doc.Issues {
		out = append(out, Finding{
			Tool:     formatGosec,
			Rule:     issue.RuleID,
			Path:     relPath(issue.File, root),
			Line:     leadingInt(issue.Line),
			Column:   leadingInt(issue.Column),
			Message:  issue.Details,
			Severity: issue.Severity + "/" + issue.Confidence,
		})
	}
	return out, nil
}

// leadingInt parses gosec's string positions, which may be a range ("12-14").
func leadingInt(s string) int {
	head, _, _ := strings.Cut(s, "-")
	n, err := strconv.Atoi(head)
	if err != nil {
		return 0
	}
	return n
}

// decodeGovulncheck reads `govulncheck -format json`, a stream of messages,
// and returns one Vuln per advisory at its deepest evidence level.
func decodeGovulncheck(r io.Reader) ([]Vuln, error) {
	type frame struct {
		Module   string
		Version  string
		Package  string
		Function string
	}
	var (
		order  []string
		vulns  = map[string]*Vuln{}
		osvs   = map[string]Vuln{}
		levels = map[string]int{levelRequired: 0, levelImported: 1, levelCalled: 2}
	)
	dec := json.NewDecoder(bufio.NewReader(r))
	for {
		var msg struct {
			OSV *struct {
				ID      string
				Aliases []string
				Summary string
			}
			Finding *struct {
				OSV          string `json:"osv"`
				FixedVersion string `json:"fixed_version"`
				Trace        []frame
			}
		}
		err := dec.Decode(&msg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("govulncheck json: %w", err)
		}
		if msg.OSV != nil {
			osvs[msg.OSV.ID] = Vuln{ID: msg.OSV.ID, Aliases: msg.OSV.Aliases, Summary: msg.OSV.Summary}
		}
		if msg.Finding == nil || len(msg.Finding.Trace) == 0 {
			continue
		}
		top := msg.Finding.Trace[0]
		level := levelRequired
		switch {
		case top.Function != "":
			level = levelCalled
		case top.Package != "":
			level = levelImported
		}
		v, seen := vulns[msg.Finding.OSV]
		if !seen {
			v = &Vuln{ID: msg.Finding.OSV, Module: top.Module, Found: top.Version, Fixed: msg.Finding.FixedVersion, Level: level}
			vulns[msg.Finding.OSV] = v
			order = append(order, msg.Finding.OSV)
		}
		if levels[level] > levels[v.Level] {
			v.Level = level
		}
	}
	out := make([]Vuln, 0, len(order))
	for _, id := range order {
		v := *vulns[id]
		if osv, ok := osvs[id]; ok {
			v.Aliases, v.Summary = osv.Aliases, osv.Summary
		}
		out = append(out, v)
	}
	return out, nil
}

// decodeFile picks the decoder for format and rejects empty input: an empty
// file means the tool never wrote its report, which is not "no findings".
func decodeFile(format string, data []byte, root string) ([]Finding, error) {
	dec, ok := decoders[format]
	if !ok {
		return nil, fmt.Errorf("unknown format %q", format)
	}
	if len(bytes.TrimSpace(data)) == 0 && format != formatStaticcheck {
		return nil, errors.New("empty report")
	}
	return dec(bytes.NewReader(data), root)
}
