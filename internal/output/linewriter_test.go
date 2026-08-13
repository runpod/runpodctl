package output

import (
	"encoding/json"

	"strings"
	"testing"
)

// record mirrors the log entry shape: json tags with omitempty, and a mixed-case
// key that yaml would otherwise mangle.
type record struct {
	Source   string `json:"source,omitempty"`
	Line     string `json:"line,omitempty"`
	WorkerID string `json:"workerId,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

// captureStdout lives in output_test.go, which is part of this same test package.

// json format must be one compact object per line: that is what makes the output
// pipeable into jq line by line, and an indented document would not be.
func TestLineWriterEmitsJSONLines(t *testing.T) {
	got := captureStdout(t, func() {
		writer := NewLineWriter(&Config{Format: FormatJSON})
		_ = writer.Write(record{Source: "container", Line: "first"})
		_ = writer.Write(record{Source: "system", Line: "second"})
		_ = writer.Close()
	})

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2: %q", len(lines), got)
	}
	for i, line := range lines {
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line %d is not standalone json: %q", i, line)
		}
	}
	if !strings.Contains(lines[0], `"line":"first"`) {
		t.Errorf("first line = %q", lines[0])
	}
}

// Regression: encoding a struct straight into yaml.v3 renamed workerId to
// workerid and printed every empty optional field, because yaml.v3 does not read
// json tags.
func TestLineWriterYAMLUsesJSONKeysAndDropsEmpties(t *testing.T) {
	got := captureStdout(t, func() {
		writer := NewLineWriter(&Config{Format: FormatYAML})
		_ = writer.Write(record{Source: "container", Line: "hello", WorkerID: "w1"})
		_ = writer.Close()
	})

	if !strings.Contains(got, "workerId: w1") {
		t.Errorf("output should carry the json key workerId:\n%s", got)
	}
	if strings.Contains(got, "workerid") {
		t.Errorf("yaml lowercased the key:\n%s", got)
	}
	if strings.Contains(got, "raw:") {
		t.Errorf("empty optional field should be omitted:\n%s", got)
	}
}

func TestLineWriterYAMLSeparatesDocuments(t *testing.T) {
	got := captureStdout(t, func() {
		writer := NewLineWriter(&Config{Format: FormatYAML})
		_ = writer.Write(record{Line: "one"})
		_ = writer.Write(record{Line: "two"})
		_ = writer.Close()
	})

	if !strings.Contains(got, "---") {
		t.Errorf("expected a document separator between records:\n%s", got)
	}
}

func TestLineWriterNilConfigDefaultsToJSON(t *testing.T) {
	got := captureStdout(t, func() {
		writer := NewLineWriter(nil)
		_ = writer.Write(record{Line: "x"})
		_ = writer.Close()
	})

	if !strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Errorf("want json by default, got:\n%s", got)
	}
}
