package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// excerptLimit is the most an error message may carry of a response body: the
// excerpt plus room for the message's own words and the truncation marker.
const excerptLimit = errorBodyExcerptBytes + 256

// TestErrorMessagesBoundTheResponseBody pins that no error path echoes a whole
// (up to 32 MiB) response body: a multi-MB response with the expected field
// missing, or with a non-200 status, yields an error of bounded size. One case
// per file that formats a body into an error.
func TestErrorMessagesBoundTheResponseBody(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	pad := strings.Repeat("x", 3<<20)
	padded := []byte(`{"data":{"pad":"` + pad + `"}}`)
	nullData := []byte(`{"data":null,"pad":"` + pad + `"}`)
	serve := func(status int, payload []byte) string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			if _, err := w.Write(payload); err != nil {
				return
			}
		}))
		t.Cleanup(server.Close)
		return server.URL
	}
	okURL := serve(http.StatusOK, padded)
	nullURL := serve(http.StatusOK, nullData)
	badURL := serve(http.StatusBadGateway, padded)

	for name, tc := range map[string]struct {
		url  string
		call func() error
	}{
		"cloud.go gpuTypes is nil":             {okURL, func() error { _, err := GetCloud(&GetCloudInput{}); return err }},
		"pod.go data is nil":                   {okURL, func() error { _, err := GetPods(); return err }},
		"pod.go pod is nil":                    {okURL, func() error { _, err := CreatePod(&CreatePodInput{}); return err }},
		"pod.go podBidResume is nil":           {okURL, func() error { _, err := StartSpotPod("p", 1, 1); return err }},
		"volume.go data is nil":                {okURL, func() error { _, err := GetNetworkVolumes(); return err }},
		"endpoint.go template is nil":          {okURL, func() error { _, err := CreateTemplate(&CreateTemplateInput{}); return err }},
		"endpoint.go endpoint is nil":          {okURL, func() error { _, err := CreateEndpoint(&CreateEndpointInput{}); return err }},
		"endpoint.go data is nil":              {okURL, func() error { _, err := GetEndpoints(); return err }},
		"graphql_response.go statuscode":       {badURL, func() error { _, _, err := queryGraphQLData(Input{Query: "q"}); return err }},
		"graphql_response.go data is nil":      {nullURL, func() error { _, _, err := queryGraphQLData(Input{Query: "q"}); return err }},
		"envOverride.go statuscode":            {badURL, func() error { _, err := GetEndpointEnvOverride("ep"); return err }},
		"modelStatus.go statuscode (refs)":     {badURL, func() error { _, err := GetEndpointModelReferences("ep"); return err }},
		"modelStatus.go statuscode (versions)": {badURL, func() error { _, err := GetPodModelVersions("pod"); return err }},
		"model.go statuscode":                  {badURL, func() error { _, err := CompleteModelRepoUploadBatch([]string{"s"}); return err }},
		"model.go data is nil":                 {nullURL, func() error { _, err := CompleteModelRepoUploadBatch([]string{"s"}); return err }},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("RUNPOD_GRAPHQL_URL", tc.url)
			err := tc.call()
			if err == nil {
				t.Fatal("expected an error")
			}
			if len(err.Error()) > excerptLimit {
				t.Fatalf("error message is %d bytes; expected the body to be cut near %d", len(err.Error()), errorBodyExcerptBytes)
			}
			if !strings.Contains(err.Error(), "truncated") {
				t.Errorf("expected a truncation marker, got %.200q", err.Error())
			}
		})
	}
}

func TestErrorBodyExcerpt(t *testing.T) {
	if got := errorBodyExcerpt([]byte("short body")); got != "short body" {
		t.Errorf("short body should pass through, got %q", got)
	}

	const sig = "X-Amz-Signature=abcdef0123456789"
	body := []byte(`{"url":"https://bucket.s3.amazonaws.com/key?` + sig + `"}`)
	if got := errorBodyExcerpt(body); strings.Contains(got, "abcdef0123456789") {
		t.Errorf("excerpt leaks a presigned signature: %q", got)
	}

	// a signature straddling the cut must already be redacted whole. The padding
	// is computed from the real prefix and signature lengths so that the cut
	// (byte errorBodyExcerptBytes of the raw body) falls in the middle of the
	// signature value, not before or after it.
	const (
		straddlePrefix = "https://b.s3.amazonaws.com/k?X-Amz-Signature="
		straddleValue  = "abcdef0123456789"
	)
	padding := errorBodyExcerptBytes - len(straddlePrefix) - len(straddleValue)/2
	straddle := []byte(strings.Repeat("a", padding) + straddlePrefix + straddleValue + strings.Repeat("b", 100))
	valueStart := bytes.Index(straddle, []byte(straddleValue))
	if valueStart >= errorBodyExcerptBytes || valueStart+len(straddleValue) <= errorBodyExcerptBytes {
		t.Fatalf("test premise: the signature value [%d,%d) must cross the cut at %d", valueStart, valueStart+len(straddleValue), errorBodyExcerptBytes)
	}
	got := errorBodyExcerpt(straddle)
	if len(got) > errorBodyExcerptBytes+len("... (truncated)") {
		t.Errorf("excerpt is %d bytes, want at most %d", len(got), errorBodyExcerptBytes+len("... (truncated)"))
	}
	for _, fragment := range []string{"abcdef", "0123456789", straddleValue[:4]} {
		if strings.Contains(got, fragment) {
			t.Errorf("excerpt leaks signature fragment %q cut by the boundary: %q", fragment, got[max(0, len(got)-80):])
		}
	}

	// multi-byte text cut mid-rune stays valid utf-8.
	multi := []byte(strings.Repeat("é", errorBodyExcerptBytes))
	if got := errorBodyExcerpt(multi); !strings.HasSuffix(got, "... (truncated)") || strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("expected valid truncated text, got suffix %q", got[len(got)-40:])
	}
}
