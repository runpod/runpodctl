package output

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format represents the output format
type Format string

const (
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatTable Format = "table"
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
	case FormatJSON:
		return printJSON(data)
	case FormatYAML:
		return printYAML(data)
	case FormatTable:
		return printTable(data)
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

func printTable(data interface{}) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	switch v := data.(type) {
	case []map[string]interface{}:
		if len(v) == 0 {
			fmt.Fprintln(os.Stdout, "no results")
			return nil
		}
		// Print headers from first item
		headers := make([]string, 0)
		for k := range v[0] {
			headers = append(headers, k)
		}
		for i, h := range headers {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			fmt.Fprint(w, h)
		}
		fmt.Fprintln(w)
		// Print rows
		for _, row := range v {
			for i, h := range headers {
				if i > 0 {
					fmt.Fprint(w, "\t")
				}
				fmt.Fprintf(w, "%v", row[h])
			}
			fmt.Fprintln(w)
		}
	default:
		// Fall back to JSON for complex types
		return printJSON(data)
	}

	return nil
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
	case "json":
		return FormatJSON
	case "yaml":
		return FormatYAML
	case "table":
		return FormatTable
	default:
		return FormatJSON
	}
}
