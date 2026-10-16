package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestFirstGraphQLErrorMessage_MalformedDoesNotPanic pins that a graphql error
// whose "message" is missing or not a string yields a fallback string instead of
// panicking. The old inline code did firstErr["message"].(string), an unchecked
// assertion that panicked on such a payload.
func TestFirstGraphQLErrorMessage_MalformedDoesNotPanic(t *testing.T) {
	cases := []struct {
		name string
		in   []any
	}{
		{"missing message", []any{map[string]any{"code": "X"}}},
		{"non-string message", []any{map[string]any{"message": 42}}},
		{"first not an object", []any{"just a string"}},
		{"empty slice", []any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := firstGraphQLErrorMessage(tc.in) // must not panic
			if got == "" {
				t.Errorf("expected a non-empty fallback message, got empty")
			}
		})
	}
}

func TestFirstGraphQLErrorMessage_Valid(t *testing.T) {
	got := firstGraphQLErrorMessage([]any{
		map[string]any{"message": "gpu unavailable"},
	})
	if got != "gpu unavailable" {
		t.Errorf("expected 'gpu unavailable', got %q", got)
	}
}

// TestParseGraphQLData_SurfacesErrorMessage pins that a graphql errors payload is
// turned into an error carrying the message, and the data map is nil.
func TestParseGraphQLData_SurfacesErrorMessage(t *testing.T) {
	data, _, err := parseGraphQLData(http.StatusOK, strings.NewReader(`{"errors":[{"message":"boom"}]}`))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing 'boom', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data on error, got %v", data)
	}
}

func TestParseGraphQLData_ReturnsDataMap(t *testing.T) {
	data, _, err := parseGraphQLData(http.StatusOK, strings.NewReader(`{"data":{"podStop":{"id":"abc"}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inner, ok := data["podStop"].(map[string]any)
	if !ok || inner["id"] != "abc" {
		t.Errorf("expected podStop.id=abc, got %v", data)
	}
}

// TestParseGraphQLData_NonOKIncludesBody pins that a non-200 response keeps the
// server's message in the error, matching the old "statuscode N: <body>" shape.
func TestParseGraphQLData_NonOKIncludesBody(t *testing.T) {
	const body = `{"error":"invalid api key"}`
	_, _, err := parseGraphQLData(http.StatusUnauthorized, strings.NewReader(body))
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	want := "statuscode 401: " + body
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestParseGraphQLData_BodyIsBounded pins that an oversized body is truncated
// rather than buffered in full.
func TestParseGraphQLData_BodyIsBounded(t *testing.T) {
	_, raw, err := parseGraphQLData(http.StatusBadGateway, strings.NewReader(strings.Repeat("x", maxGraphQLBodyBytes*2)))
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if len(raw) != maxGraphQLBodyBytes {
		t.Errorf("read %d bytes, want %d", len(raw), maxGraphQLBodyBytes)
	}
}
