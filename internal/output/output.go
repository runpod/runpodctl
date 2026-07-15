package output

import (
	"encoding/json"
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// Format represents the output format
type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// Config holds output configuration
type Config struct {
	Format Format
}

// DefaultConfig returns the default output config (JSON for agents)
var DefaultConfig = &Config{Format: FormatJSON}

// Print outputs data in the configured format
func Print(data interface{}, cfg *Config) error {
	if cfg == nil {
		cfg = DefaultConfig
	}
	data = normalizeGPUKeys(data)

	switch cfg.Format {
	case FormatYAML:
		return printYAML(data)
	default:
		return printJSON(data)
	}
}

func printJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func printYAML(data interface{}) error {
	encoder := yaml.NewEncoder(os.Stdout)
	encoder.SetIndent(2)
	return encoder.Encode(data)
}

// errorObject is the flat, stable JSON shape emitted for cli errors so agents
// can branch on a machine-readable code without parsing the message string.
type errorObject struct {
	Error  string `json:"error"`
	Code   string `json:"code,omitempty"`
	Status int    `json:"status,omitempty"`
}

// Error writes a single flat JSON error object to stderr. When the error (or an
// error it wraps) exposes a stable code or HTTP status, those are included.
func Error(err error) {
	if err == nil {
		return
	}

	obj := errorObject{Error: err.Error()}

	var coder interface{ ErrorCode() string }
	if errors.As(err, &coder) {
		obj.Code = coder.ErrorCode()
	}
	var statuser interface{ HTTPStatus() int }
	if errors.As(err, &statuser) {
		obj.Status = statuser.HTTPStatus()
	}

	encoder := json.NewEncoder(os.Stderr)
	encoder.Encode(obj) //nolint:errcheck
}

// ParseFormat parses a format string into a Format
func ParseFormat(s string) Format {
	switch s {
	case "yaml":
		return FormatYAML
	default:
		return FormatJSON
	}
}

func normalizeGPUKeys(data interface{}) interface{} {
	if data == nil {
		return data
	}

	switch data.(type) {
	case map[string]interface{}, []interface{}:
		return renameGPUKeys(data)
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return data
		}
		var decoded interface{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return data
		}
		return renameGPUKeys(decoded)
	}
}

func renameGPUKeys(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		updated := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			newKey := key
			switch key {
			case "gpuTypeId":
				newKey = "gpuId"
			case "gpuTypeIds":
				newKey = "gpuIds"
			}
			updated[newKey] = renameGPUKeys(val)
		}
		return updated
	case []interface{}:
		for i, item := range typed {
			typed[i] = renameGPUKeys(item)
		}
		return typed
	default:
		return value
	}
}
