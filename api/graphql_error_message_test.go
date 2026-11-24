package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

const errorMessageTestSignature = "deadbeefcafe0123456789"

// hugeSignedErrorMessage is a graphql error message with a presigned url near
// its start and megabytes of padding after it.
func hugeSignedErrorMessage() string {
	return "upload failed https://bucket.s3.amazonaws.com/key?X-Amz-Credential=AKIAEXAMPLE&X-Amz-Signature=" +
		errorMessageTestSignature + " " + strings.Repeat("m", 3<<20)
}

// TestTopLevelGraphQLErrorMessagesAreBoundedAndRedacted pins that a 200 response
// whose errors[0].message is oversized and carries a presigned url is cut and
// redacted on every api path that surfaces that message.
func TestTopLevelGraphQLErrorMessagesAreBoundedAndRedacted(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	body, err := json.Marshal(map[string]any{"errors": []map[string]any{{"message": hugeSignedErrorMessage()}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, writeErr := w.Write(body); writeErr != nil {
			return
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	for name, call := range map[string]func() error{
		"pod.go":                      func() error { _, e := GetPods(); return e },
		"volume.go":                   func() error { _, e := GetNetworkVolumes(); return e },
		"endpoint.go":                 func() error { _, e := GetEndpoints(); return e },
		"envOverride.go":              func() error { _, e := GetEndpointEnvOverride("ep"); return e },
		"modelStatus.go (refs)":       func() error { _, e := GetEndpointModelReferences("ep"); return e },
		"modelStatus.go (versions)":   func() error { _, e := GetPodModelVersions("pod"); return e },
		"graphql_response.go":         func() error { _, _, e := queryGraphQLData(Input{Query: "q"}); return e },
		"model.go (batch complete)":   func() error { _, e := CompleteModelRepoUploadBatch([]string{"s"}); return e },
		"model.go (batch, path-less)": func() error { _, e := CompleteModelRepoUploadBatch([]string{"a", "b"}); return e },
	} {
		t.Run(name, func(t *testing.T) {
			e := call()
			if e == nil {
				t.Fatal("expected an error")
			}
			got := e.Error()
			if len(got) > excerptLimit {
				t.Fatalf("message is %d bytes, want at most %d", len(got), excerptLimit)
			}
			if !strings.Contains(got, "truncated") {
				t.Errorf("expected a truncation marker, got %.200q", got)
			}
			if strings.Contains(got, errorMessageTestSignature) {
				t.Errorf("message leaks the signature: %.200q", got)
			}
		})
	}
}

// TestBatchFinalizedAliasErrorIsBoundedAndRedacted pins the path-resolved
// case: errors[i].path names the c<index> session alias, the batch-complete
// path turns it into "N of M sessions failed to finalize (alias-message)",
// and that alias message is bounded and redacted like every other server
// message (plus the joined list is bounded again at the end).
func TestBatchFinalizedAliasErrorIsBoundedAndRedacted(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	body, err := json.Marshal(map[string]any{"errors": []map[string]any{
		{"message": hugeSignedErrorMessage(), "path": []string{"c0"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, writeErr := w.Write(body); writeErr != nil {
			return
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err = CompleteModelRepoUploadBatch([]string{"session-0"})
	if err == nil {
		t.Fatal("expected an error")
	}
	got := err.Error()
	if !strings.Contains(got, "failed to finalize") {
		t.Fatalf("expected the alias-resolved failure shape, got %.200q", got)
	}
	if len(got) > excerptLimit {
		t.Fatalf("message is %d bytes, want at most %d", len(got), excerptLimit)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected a truncation marker, got %.200q", got)
	}
	if strings.Contains(got, errorMessageTestSignature) {
		t.Errorf("message leaks the signature: %.200q", got)
	}
}
