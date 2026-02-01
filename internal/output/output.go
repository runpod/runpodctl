package output

import (
	"encoding/json"
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

// Error outputs an error in JSON format to stderr
func Error(err error) {
	errObj := map[string]string{"error": err.Error()}
	encoder := json.NewEncoder(os.Stderr)
	encoder.Encode(errObj) //nolint:errcheck
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
