package output

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
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

// fallbackCode classifies errors that carry no ErrorCode() of their own, so that
// *every* emitted error object has a code. Without this, hand-written
// validation errors and transport failures came out as a bare
// {"error":"..."} and an agent had to read english to tell "i passed a bad
// flag value" from "the network is down" from "the server broke".
//
//	network_error -- could not reach the api at all (dns, refused, tls, timeout)
//	cli_error     -- everything else uncoded: local validation, config, bad input
//
// This is a fallback only; a typed error's own code always wins.
func fallbackCode(err error) string {
	if isNetworkError(err) {
		return "network_error"
	}
	return "cli_error"
}

// isNetworkError reports whether err is a failure to reach the remote at all,
// as opposed to a response from it. net/http wraps transport failures in
// *url.Error, whose Unwrap chain carries the underlying *net.OpError,
// *net.DNSError or tls error.
func isNetworkError(err error) bool {
	var urlErr *url.Error
	// url.Parse also returns *url.Error, with Op "parse" — a malformed
	// RUNPOD_API_URL is a local config mistake, not an unreachable api, and it
	// must not be reported as network_error. network_error is the one code that
	// tells an agent "transient, retry"; a permanently broken url would then be
	// retried forever instead of surfacing the real problem.
	if errors.As(err, &urlErr) && urlErr.Op != "parse" {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// Deliberately NOT matching a bare context.DeadlineExceeded or
	// io.ErrUnexpectedEOF: any local wait loop can produce those without a
	// network ever being involved (e.g. --wait-for-hash timing out after a
	// perfectly successful upload), and network_error is the one code that tells
	// an agent "transient, retry". A real http client timeout arrives wrapped in
	// *url.Error, so the branches above already cover it.
	return false
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
	if obj.Code == "" {
		obj.Code = fallbackCode(err)
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
