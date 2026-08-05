package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

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

// PrintRaw outputs a json document that came straight off the wire, without the
// gpu-key normalisation Print applies and without turning numbers into float64.
//
// It exists for payloads whose content is not ours: a serverless handler's job
// output and the invoke api's health body. Print is built for the cli's own
// typed control-plane structs, where rewriting gpuTypeId -> gpuId is wanted;
// doing that to third-party data silently renames a handler's own keys, and the
// map round trip it uses corrupts any integer above 2^53. Keys are still sorted
// (stable output across api versions) but key names and number literals are
// reproduced exactly.
func PrintRaw(raw []byte, cfg *Config) error {
	if cfg == nil {
		cfg = DefaultConfig
	}

	value, err := decodeRawJSON(raw)
	if err != nil {
		// not json: hand the bytes over untouched rather than failing the command,
		// so whatever the api said is still visible.
		_, writeErr := os.Stdout.Write(ensureTrailingNewline(raw))
		return writeErr
	}

	if cfg.Format == FormatYAML {
		return printYAML(yamlValue(value))
	}
	// json.Number marshals back as its original literal, so this is byte-faithful
	// for numbers while still sorting object keys.
	return printJSON(value)
}

// decodeRawJSON decodes a single json document, keeping numbers as their exact
// literals (json.Number) instead of float64.
func decodeRawJSON(raw []byte) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func ensureTrailingNewline(raw []byte) []byte {
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		return raw
	}
	return append(append([]byte(nil), raw...), '\n')
}

// yamlValue converts a decoded json value into a yaml node, so that yaml output
// keeps the same guarantees as the json path: object keys sorted (Go map
// iteration order is random, which yaml.Marshal would otherwise expose) and
// numbers emitted as their original literals rather than as quoted json.Number
// strings or rounded floats.
func yamlValue(value interface{}) *yaml.Node {
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, key := range keys {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
				yamlValue(typed[key]),
			)
		}
		return node
	case []interface{}:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			node.Content = append(node.Content, yamlValue(item))
		}
		return node
	case json.Number:
		tag := "!!int"
		if strings.ContainsAny(typed.String(), ".eE") {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: typed.String()}
	default:
		node := &yaml.Node{}
		// let yaml infer the tag for strings, bools and null.
		_ = node.Encode(typed)
		return node
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
	ID     string `json:"id,omitempty"`
}

// resourceIDError annotates a failure with the id of a resource that was created
// and outlives it, so the emitted error object carries the id as data.
type resourceIDError struct {
	id  string
	err error
}

func (e *resourceIDError) Error() string { return e.err.Error() }

func (e *resourceIDError) Unwrap() error { return e.err }

func (e *resourceIDError) ErrorResourceID() string { return e.id }

// WithResourceID marks err as a failure that left a resource behind, so the
// error object gains an `id` field.
//
// Anything that fails *after* a create has succeeded must use this: `pod create
// --wait` that times out has bought a pod, and an agent needs to delete it. The
// message names the id too, for humans, but prose is not something a caller
// should have to parse to avoid leaking a billed resource.
func WithResourceID(id string, err error) error {
	if err == nil || id == "" {
		return err
	}
	return &resourceIDError{id: id, err: err}
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
	// io.ErrUnexpectedEOF: any local wait loop can produce those without a network
	// ever being involved, and network_error is the one code that tells an agent
	// "transient, retry". A real http client timeout arrives wrapped in *url.Error,
	// so the branches above already cover it. A local wait that wants to be
	// machine-readable says so itself, with a typed error carrying its own code --
	// which is what --wait-for-hash now does (api.TimeoutError, code "timeout").
	return false
}

// Error writes a single flat JSON error object to stderr. When the error (or an
// error it wraps) exposes a stable code, an HTTP status or the id of a resource
// it left behind (see WithResourceID), those are included.
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
	var ider interface{ ErrorResourceID() string }
	if errors.As(err, &ider) {
		obj.ID = ider.ErrorResourceID()
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
