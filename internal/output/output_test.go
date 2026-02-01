package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected Format
	}{
		{"json", FormatJSON},
		{"yaml", FormatYAML},
		{"table", FormatTable},
		{"invalid", FormatJSON}, // defaults to json
		{"", FormatJSON},
	}

	for _, test := range tests {
		result := ParseFormat(test.input)
		if result != test.expected {
			t.Errorf("ParseFormat(%q) = %v, want %v", test.input, result, test.expected)
		}
	}
}

func TestPrint_JSON(t *testing.T) {
	// capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"id": "test-123", "name": "test"}
	err := Print(data, &Config{Format: FormatJSON})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	var result map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}
	if result["id"] != "test-123" {
		t.Errorf("expected test-123, got %s", result["id"])
	}
}

func TestPrint_YAML(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"id": "test-123"}
	err := Print(data, &Config{Format: FormatYAML})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "id: test-123") {
		t.Errorf("yaml output should contain 'id: test-123', got %s", output)
	}
}

func TestPrint_DefaultConfig(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]string{"test": "value"}
	err := Print(data, nil) // nil config should use default (json)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// should be valid json
	var result map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("default should be json: %v", err)
	}
}

func TestError(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	Error(fmt.Errorf("test error"))

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, `"error":"test error"`) {
		t.Errorf("expected error json, got %s", output)
	}
}
