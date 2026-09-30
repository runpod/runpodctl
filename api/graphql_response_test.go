package api

import (
	"io"
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
		in   []interface{}
	}{
		{"missing message", []interface{}{map[string]interface{}{"code": "X"}}},
		{"non-string message", []interface{}{map[string]interface{}{"message": 42}}},
		{"first not an object", []interface{}{"just a string"}},
		{"empty slice", []interface{}{}},
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
	got := firstGraphQLErrorMessage([]interface{}{
		map[string]interface{}{"message": "gpu unavailable"},
	})
	if got != "gpu unavailable" {
		t.Errorf("expected 'gpu unavailable', got %q", got)
	}
}

// TestParseGraphQLData_SurfacesErrorMessage pins that a graphql errors payload is
// turned into an error carrying the message, and the data map is nil.
func TestParseGraphQLData_SurfacesErrorMessage(t *testing.T) {
	res := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"errors":[{"message":"boom"}]}`)),
	}
	data, _, err := parseGraphQLData(res)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing 'boom', got %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data on error, got %v", data)
	}
}

func TestParseGraphQLData_ReturnsDataMap(t *testing.T) {
	res := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"data":{"podStop":{"id":"abc"}}}`)),
	}
	data, _, err := parseGraphQLData(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inner, ok := data["podStop"].(map[string]interface{})
	if !ok || inner["id"] != "abc" {
		t.Errorf("expected podStop.id=abc, got %v", data)
	}
}
