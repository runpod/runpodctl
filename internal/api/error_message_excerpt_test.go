package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

const excerptTestSignature = "deadbeef0123456789"

// hugeSignedMessage is a message far above the excerpt bound that starts with a
// presigned URL, so both the redaction and the bound are observable.
func hugeSignedMessage() string {
	return "failed https://bucket.s3.amazonaws.com/k?X-Amz-Signature=" + excerptTestSignature + " " + strings.Repeat("m", 3<<20)
}

func assertBoundedRedactedMessage(t *testing.T, got string) {
	t.Helper()
	if limit := ErrorBodyExcerptBytes + 256; len(got) > limit {
		t.Fatalf("message is %d bytes, want at most %d", len(got), limit)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected a truncation marker, got %.120q", got)
	}
	if strings.Contains(got, excerptTestSignature) {
		t.Errorf("message leaks the signature: %.200q", got)
	}
}

// TestExtractedErrorMessagesAreBoundedAndRedacted pins that a valid JSON error
// envelope with a multi-megabyte field is bounded and redacted too, not only the
// fallback that echoes a non-JSON body.
func TestExtractedErrorMessagesAreBoundedAndRedacted(t *testing.T) {
	huge := hugeSignedMessage()
	marshal := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	rest := map[string][]byte{
		"error":          marshal(map[string]any{"error": huge}),
		"message":        marshal(map[string]any{"message": huge}),
		"detail":         marshal(map[string]any{"detail": huge}),
		"detail+errors":  marshal(map[string]any{"detail": "Request validation failed.", "errors": []string{huge, huge}}),
		"detail,s+error": marshal(map[string]any{"detail": huge, "errors": []string{huge}}),
	}
	for name, body := range rest {
		t.Run("rest "+name, func(t *testing.T) {
			assertBoundedRedactedMessage(t, parseAPIError(body, http.StatusBadRequest).Message)
		})
	}

	graphql := map[string][]byte{
		"errors[0].message": marshal(map[string]any{"errors": []map[string]any{{"message": huge}}}),
		"error":             marshal(map[string]any{"error": huge}),
		"message":           marshal(map[string]any{"message": huge}),
	}
	for name, body := range graphql {
		t.Run("graphql "+name, func(t *testing.T) {
			assertBoundedRedactedMessage(t, extractGraphQLMessage(body))
			assertBoundedRedactedMessage(t, parseGraphQLHTTPError(body, http.StatusBadRequest).Message)
		})
	}

	t.Run("newGraphQLError", func(t *testing.T) {
		assertBoundedRedactedMessage(t, newGraphQLError(huge).Message)
	})
	t.Run("short messages pass through", func(t *testing.T) {
		if got := parseAPIError([]byte(`{"error":"pod not found"}`), 404).Message; got != "pod not found" {
			t.Errorf("got %q", got)
		}
		if got := extractGraphQLMessage([]byte(`{"errors":[{"message":"nope"}]}`)); got != "nope" {
			t.Errorf("got %q", got)
		}
	})
}

// TestClientsBoundValidJSONErrorEnvelopes drives the same through the real
// rest and graphql clients.
func TestClientsBoundValidJSONErrorEnvelopes(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	huge := hugeSignedMessage()
	restBody, err := json.Marshal(map[string]any{"error": huge})
	if err != nil {
		t.Fatal(err)
	}
	gqlBody, err := json.Marshal(map[string]any{"errors": []map[string]any{{"message": huge}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := restBody
		if r.Method == http.MethodPost {
			body = gqlBody
		}
		w.WriteHeader(http.StatusBadRequest)
		if _, writeErr := w.Write(body); writeErr != nil {
			return
		}
	}))
	defer server.Close()
	t.Setenv("RUNPOD_API_URL", server.URL)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	rest, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, restErr := rest.GetV2("/pods", nil)
	if restErr == nil {
		t.Fatal("rest: expected an error")
	}
	assertBoundedRedactedMessage(t, restErr.Error())

	gql, err := NewGraphQLClient()
	if err != nil {
		t.Fatalf("NewGraphQLClient: %v", err)
	}
	_, gqlErr := gql.Query(GraphQLInput{Query: "query test"})
	if gqlErr == nil {
		t.Fatal("graphql: expected an error")
	}
	assertBoundedRedactedMessage(t, gqlErr.Error())
}
