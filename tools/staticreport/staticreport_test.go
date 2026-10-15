package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFindingKey(t *testing.T) {
	for _, tc := range []struct {
		description string
		finding     Finding
		expected    string
	}{
		{
			description: "plain message",
			finding:     Finding{Path: "cmd/a.go", Message: "unchecked error", Rule: "errcheck"},
			expected:    "cmd/a.go | unchecked error (errcheck)",
		},
		{
			description: "digits read as N, so a count change is not a new finding",
			finding:     Finding{Path: "a.go", Message: "naked return in func `f` with 45 lines of code", Rule: "nakedret"},
			expected:    "a.go | naked return in func `f` with N lines of code (nakedret)",
		},
		{
			description: "multi-line message collapses to one line",
			finding:     Finding{Path: "a.go", Message: "suggest:\n\t\tx :=\ty", Rule: "gocritic"},
			expected:    "a.go | suggest: x := y (gocritic)",
		},
		{
			description: "line and column are not part of the key",
			finding:     Finding{Path: "a.go", Line: 99, Column: 7, Message: "m", Rule: "r"},
			expected:    "a.go | m (r)",
		},
		{
			description: "empty message",
			finding:     Finding{Path: "a.go", Rule: "r"},
			expected:    "a.go |  (r)",
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.finding.Key(); got != tc.expected {
				t.Errorf("Key() = %q, expected %q", got, tc.expected)
			}
		})
	}
}

func TestFindingPackage(t *testing.T) {
	for _, tc := range []struct {
		description string
		path        string
		expected    string
	}{
		{"nested package", "cmd/project/ssh.go", "cmd/project"},
		{"root file", "main.go", "(root)"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := (&Finding{Path: tc.path}).Package(); got != tc.expected {
				t.Errorf("Package() = %q, expected %q", got, tc.expected)
			}
		})
	}
}

func TestRelPath(t *testing.T) {
	for _, tc := range []struct {
		description string
		path, root  string
		expected    string
	}{
		{"absolute under root", "/build/src/cmd/a.go", "/build/src", "cmd/a.go"},
		{"already relative", "cmd/a.go", "/build/src", "cmd/a.go"},
		{"absolute outside root is kept", "/nix/store/x/go/src/fmt/print.go", "/build/src", "/nix/store/x/go/src/fmt/print.go"},
		{"no root", "/build/src/a.go", "", "/build/src/a.go"},
		{"cleaned", "cmd/./a.go", "", "cmd/a.go"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := relPath(tc.path, tc.root); got != tc.expected {
				t.Errorf("relPath(%q, %q) = %q, expected %q", tc.path, tc.root, got, tc.expected)
			}
		})
	}
}

func TestDecodeFile(t *testing.T) {
	for _, tc := range []struct {
		description string
		format      string
		input       string
		expected    []Finding
		expectErr   bool
	}{
		{
			description: "golangci issue",
			format:      "golangci",
			input:       `{"Issues":[{"FromLinter":"errcheck","Text":"unchecked","Severity":"","Pos":{"Filename":"api/cloud.go","Line":40,"Column":22}}]}`,
			expected:    []Finding{{Tool: "golangci", Rule: "errcheck", Path: "api/cloud.go", Line: 40, Column: 22, Message: "unchecked"}},
		},
		{
			description: "golangci with no issues",
			format:      "golangci",
			input:       `{"Issues":[]}`,
			expected:    []Finding{},
		},
		{
			description: "golangci truncated json",
			format:      "golangci",
			input:       `{"Issues":[`,
			expectErr:   true,
		},
		{
			description: "empty golangci report is not zero findings",
			format:      "golangci",
			input:       " \n",
			expectErr:   true,
		},
		{
			description: "staticcheck json lines with absolute paths",
			format:      "staticcheck",
			input: `{"code":"ST1000","severity":"error","location":{"file":"/r/api/cloud.go","line":1,"column":1},"message":"package comment"}
{"code":"SA4006","severity":"error","location":{"file":"/r/main.go","line":3,"column":2},"message":"never used"}`,
			expected: []Finding{
				{Tool: "staticcheck", Rule: "ST1000", Path: "api/cloud.go", Line: 1, Column: 1, Message: "package comment", Severity: "error"},
				{Tool: "staticcheck", Rule: "SA4006", Path: "main.go", Line: 3, Column: 2, Message: "never used", Severity: "error"},
			},
		},
		{
			description: "staticcheck with no findings writes nothing",
			format:      "staticcheck",
			input:       "",
			expected:    nil,
		},
		{
			description: "staticcheck garbage",
			format:      "staticcheck",
			input:       "not json",
			expectErr:   true,
		},
		{
			description: "gosec issue with a line range",
			format:      "gosec",
			input:       `{"Golang errors":{},"Issues":[{"severity":"HIGH","confidence":"MEDIUM","rule_id":"G115","details":"overflow","file":"/r/cmd/x.go","line":"273-275","column":"19"}]}`,
			expected:    []Finding{{Tool: "gosec", Rule: "G115", Path: "cmd/x.go", Line: 273, Column: 19, Message: "overflow", Severity: "HIGH/MEDIUM"}},
		},
		{
			description: "gosec load errors make its results incomplete",
			format:      "gosec",
			input:       `{"Golang errors":{"/r/a.go":[{"line":1,"column":1,"error":"x"}]},"Issues":[]}`,
			expectErr:   true,
		},
		{
			description: "unknown format",
			format:      "eslint",
			input:       "{}",
			expectErr:   true,
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			got, err := decodeFile(tc.format, []byte(tc.input), "/r")
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %#v\nexpected %#v", got, tc.expected)
			}
		})
	}
}

func TestDecodeGovulncheck(t *testing.T) {
	osv := `{"osv":{"id":"GO-1","aliases":["CVE-1"],"summary":"bad"}}`
	for _, tc := range []struct {
		description string
		input       string
		expected    []Vuln
		expectErr   bool
	}{
		{
			description: "no findings",
			input:       `{"config":{}}` + "\n" + osv,
			expected:    []Vuln{},
		},
		{
			description: "module level only",
			input:       osv + `{"finding":{"osv":"GO-1","fixed_version":"v2","trace":[{"module":"m","version":"v1"}]}}`,
			expected:    []Vuln{{ID: "GO-1", Aliases: []string{"CVE-1"}, Summary: "bad", Module: "m", Found: "v1", Fixed: "v2", Level: "required"}},
		},
		{
			description: "deepest level wins, whatever the order",
			input: osv +
				`{"finding":{"osv":"GO-1","trace":[{"module":"m","version":"v1","package":"m/p","function":"F"}]}}` +
				`{"finding":{"osv":"GO-1","trace":[{"module":"m","version":"v1","package":"m/p"}]}}`,
			expected: []Vuln{{ID: "GO-1", Aliases: []string{"CVE-1"}, Summary: "bad", Module: "m", Found: "v1", Level: "called"}},
		},
		{
			description: "finding without a trace is ignored",
			input:       `{"finding":{"osv":"GO-2","trace":[]}}`,
			expected:    []Vuln{},
		},
		{
			description: "truncated stream",
			input:       `{"finding":{"osv":`,
			expectErr:   true,
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			got, err := decodeGovulncheck(strings.NewReader(tc.input))
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %#v\nexpected %#v", got, tc.expected)
			}
		})
	}
}

func TestParseBaseline(t *testing.T) {
	for _, tc := range []struct {
		description string
		input       string
		expected    Baseline
		expectErr   bool
	}{
		{
			description: "header only",
			input:       baselineHeader,
			expected:    Baseline{},
		},
		{
			description: "repeated line counts twice",
			input:       "# c\n\ngosec\ta.go | x (G1)\ngosec\ta.go | x (G1)\ntier0\tb.go | y (errcheck)\n",
			expected:    Baseline{"gosec": {"a.go | x (G1)": 2}, "tier0": {"b.go | y (errcheck)": 1}},
		},
		{
			description: "unsorted lines are rejected",
			input:       "tier0\tb.go | y (r)\ngosec\ta.go | x (r)\n",
			expectErr:   true,
		},
		{
			description: "line without a tab",
			input:       "tier0 a.go | x (r)\n",
			expectErr:   true,
		},
		{
			description: "empty key",
			input:       "tier0\t\n",
			expectErr:   true,
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseBaseline(strings.NewReader(tc.input))
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	measured := map[string][]Finding{
		"tier0": {{Path: "b.go", Message: "y", Rule: "r"}, {Path: "a.go", Message: "x", Rule: "r"}, {Path: "a.go", Message: "x", Rule: "r"}},
		"gosec": {{Path: "c.go", Message: "line 12\nnext", Rule: "G1"}},
	}
	var buf bytes.Buffer
	if err := writeBaseline(&buf, measured); err != nil {
		t.Fatal(err)
	}
	got, err := parseBaseline(&buf)
	if err != nil {
		t.Fatalf("parsing written baseline: %v", err)
	}
	for tool, findings := range measured {
		if d := compare(got[tool], findings); len(d.New) != 0 || len(d.Fixed) != 0 {
			t.Errorf("%s: round trip is not exact: %+v", tool, d)
		}
	}
}

func TestCompare(t *testing.T) {
	a := Finding{Path: "a.go", Message: "x", Rule: "r"}
	b := Finding{Path: "b.go", Message: "y", Rule: "r"}
	moved := a
	moved.Line = 500
	for _, tc := range []struct {
		description string
		baseline    map[string]int
		findings    []Finding
		expected    Delta
	}{
		{"identical", map[string]int{a.Key(): 1}, []Finding{a}, Delta{}},
		{"moved line is the same finding", map[string]int{a.Key(): 1}, []Finding{moved}, Delta{}},
		{"new finding", map[string]int{a.Key(): 1}, []Finding{a, b}, Delta{New: []string{b.Key()}}},
		{"fixed finding", map[string]int{a.Key(): 1, b.Key(): 1}, []Finding{a}, Delta{Fixed: []string{b.Key()}}},
		{"second copy of a known finding is new", map[string]int{a.Key(): 1}, []Finding{a, a}, Delta{New: []string{a.Key()}}},
		{"fix one and add one still fails", map[string]int{a.Key(): 1}, []Finding{b}, Delta{New: []string{b.Key()}, Fixed: []string{a.Key()}}},
		{"no baseline for the tool", nil, []Finding{a}, Delta{New: []string{a.Key()}}},
		{"empty both", nil, nil, Delta{}},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := compare(tc.baseline, tc.findings); !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("compare() = %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

func TestRunBaselineExitCodes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	one := write("one.json", `{"Issues":[{"FromLinter":"errcheck","Text":"x","Pos":{"Filename":"a.go","Line":1}}]}`)
	two := write("two.json", `{"Issues":[{"FromLinter":"errcheck","Text":"x","Pos":{"Filename":"a.go","Line":1}},{"FromLinter":"unused","Text":"y","Pos":{"Filename":"b.go","Line":2}}]}`)
	none := write("none.json", `{"Issues":[]}`)
	baseline := write("baseline.txt", baselineHeader+"tier0\ta.go | x (errcheck)\n")
	other := write("other.txt", "gosec\ta.go | x (G1)\n")

	for _, tc := range []struct {
		description string
		args        []string
		expected    int
	}{
		{"matches", []string{"-baseline", baseline, "-in", "tier0=golangci:" + one, "-exit", "tier0=1"}, exitOK},
		{"new finding", []string{"-baseline", baseline, "-in", "tier0=golangci:" + two, "-exit", "tier0=1"}, exitFindings},
		{"fixed but baseline not shrunk", []string{"-baseline", baseline, "-in", "tier0=golangci:" + none, "-exit", "tier0=0"}, exitFindings},
		{"tool crashed", []string{"-baseline", baseline, "-in", "tier0=golangci:" + one, "-exit", "tier0=3"}, exitToolFail},
		{"exit 1 with no findings is a lost report", []string{"-baseline", baseline, "-in", "tier0=golangci:" + none, "-exit", "tier0=1"}, exitToolFail},
		{"missing exit status", []string{"-baseline", baseline, "-in", "tier0=golangci:" + one}, exitUsage},
		{"missing input file", []string{"-baseline", baseline, "-in", "tier0=golangci:" + filepath.Join(dir, "nope"), "-exit", "tier0=1"}, exitUsage},
		{"missing baseline", []string{"-baseline", filepath.Join(dir, "nope"), "-in", "tier0=golangci:" + one, "-exit", "tier0=1"}, exitUsage},
		{"baseline names an unmeasured tool", []string{"-baseline", other, "-in", "tier0=golangci:" + none, "-exit", "tier0=0"}, exitUsage},
		{"no inputs", []string{"-baseline", baseline}, exitUsage},
		{"bad -in spec", []string{"-baseline", baseline, "-in", "tier0", "-exit", "tier0=1"}, exitUsage},
	} {
		t.Run(tc.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(append([]string{"baseline"}, tc.args...), &stdout, &stderr); got != tc.expected {
				t.Errorf("exit %d, expected %d\nstdout: %s\nstderr: %s", got, tc.expected, stdout.String(), stderr.String())
			}
		})
	}

	t.Run("update writes a baseline that then matches", func(t *testing.T) {
		path := filepath.Join(dir, "updated.txt")
		args := []string{"-baseline", path, "-in", "tier0=golangci:" + two, "-exit", "tier0=1"}
		var out bytes.Buffer
		if got := run(append([]string{"baseline", "-update"}, args...), &out, &out); got != exitOK {
			t.Fatalf("update exit %d: %s", got, out.String())
		}
		if got := run(append([]string{"baseline"}, args...), &out, &out); got != exitOK {
			t.Fatalf("check after update exit %d: %s", got, out.String())
		}
	})
}

func TestRunReport(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "gosec.json")
	if err := os.WriteFile(in, []byte(`{"Issues":[{"severity":"HIGH","confidence":"HIGH","rule_id":"G304","details":"file inclusion | via var","file":"cmd/a.go","line":"3","column":"1"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(baseline, []byte(baselineHeader), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(dir, "meta.txt")
	if err := os.WriteFile(meta, []byte("rev=abc\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "report.md")
	var stdout, stderr bytes.Buffer
	rc := run([]string{"report", "-baseline", baseline, "-in", "gosec=gosec:" + in, "-exit", "gosec=0", "-meta-file", meta, "-md", md, "-json", filepath.Join(dir, "report.json")}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d: %s", rc, stderr.String())
	}
	data, err := os.ReadFile(filepath.Clean(md))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		description string
		expected    string
	}{
		{"provenance", "| rev | `abc` |"},
		{"summary row", "| gosec | 0 | 1 | 1 | 0 |"},
		{"govulncheck not run is explicit", "Not run"},
		{"gosec by severity", "| G304 HIGH/HIGH | 1 |"},
		{"new finding listed, pipe escaped", "file inclusion \\| via var"},
		{"package table", "| `cmd` | 1 |"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if !strings.Contains(string(data), tc.expected) {
				t.Errorf("report lacks %q:\n%s", tc.expected, data)
			}
		})
	}
}
