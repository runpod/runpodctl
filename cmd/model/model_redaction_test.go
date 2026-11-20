package model

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

const testPresignedQuery = "?partNumber=1&uploadId=U1&X-Amz-Algorithm=AWS4-HMAC-SHA256" +
	"&X-Amz-Credential=AKIAEXAMPLE%2F20260101%2Fus-east-1%2Fs3%2Faws4_request" +
	"&X-Amz-Security-Token=TOKENVALUE&X-Amz-Signature=deadbeef0123456789"

func assertNoPresignedSecrets(t *testing.T, msg string) {
	t.Helper()
	for _, secret := range []string{"deadbeef", "AKIAEXAMPLE", "TOKENVALUE"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error output leaks %q:\n%s", secret, msg)
		}
	}
}

func writeSmallArtifact(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}

func TestCompleteModelUpload_ConnectionFailureRedactsPresignedURL(t *testing.T) {
	fastUploadBackoff(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	partURL := closed.URL + "/bucket/key.bin" + testPresignedQuery
	closed.Close() // every attempt fails at connect, so *url.Error quotes the URL

	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   closed.URL + "/complete" + testPresignedQuery,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: partURL}},
	}, writeSmallArtifact(t), nil)
	if err == nil {
		t.Fatal("expected a connection failure")
	}
	assertNoPresignedSecrets(t, err.Error())
	if strings.Contains(err.Error(), "uploadId") || strings.Contains(err.Error(), "?") {
		t.Fatalf("the query string should be dropped entirely: %v", err)
	}
	if !strings.Contains(err.Error(), "/bucket/key.bin") {
		t.Fatalf("the host/path should stay for diagnosis: %v", err)
	}
}

func TestCompleteModelUpload_MalformedPresignedURLRedacted(t *testing.T) {
	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   "http://127.0.0.1:1",
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: "https://bad host/key" + testPresignedQuery}},
	}, writeSmallArtifact(t), nil)
	if err == nil {
		t.Fatal("expected a request-construction failure")
	}
	assertNoPresignedSecrets(t, err.Error())
}

const s3ErrorBody = `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>SignatureDoesNotMatch</Code><Message>The request signature we calculated does not match the signature you provided.</Message>` +
	`<AWSAccessKeyId>AKIAEXAMPLE</AWSAccessKeyId><StringToSign>AWS4-HMAC-SHA256 deadbeef0123456789</StringToSign>` +
	`<SignatureProvided>deadbeef0123456789</SignatureProvided><RequestId>R1</RequestId></Error>`

func writeS3ErrorBody(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	if _, err := w.Write([]byte(s3ErrorBody)); err != nil {
		t.Errorf("write error body: %v", err)
	}
}

func TestCompleteModelUpload_StorageErrorBodyIsSummarized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeS3ErrorBody(t, w)
	}))
	t.Cleanup(server.Close)

	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL + testPresignedQuery}},
	}, writeSmallArtifact(t), nil)
	if err == nil {
		t.Fatal("expected a 403 to fail the upload")
	}
	msg := err.Error()
	assertNoPresignedSecrets(t, msg)
	for _, leak := range []string{"StringToSign", "SignatureProvided", "<Error>"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("raw storage body leaked %q: %s", leak, msg)
		}
	}
	if !strings.Contains(msg, "status 403") || !strings.Contains(msg, "SignatureDoesNotMatch") ||
		!strings.Contains(msg, "request signature we calculated") {
		t.Fatalf("status, code and message should remain: %s", msg)
	}
}

func TestCompleteModelUpload_CompletionErrorBodyIsSummarized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			drainRequestBody(t, r)
			w.Header().Set("ETag", `"etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		writeS3ErrorBody(t, w)
	}))
	t.Cleanup(server.Close)

	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 6,
		CompleteURL:   server.URL + "/complete" + testPresignedQuery,
		Parts:         []*api.ModelRepoUploadPart{{PartNumber: 1, URL: server.URL}},
	}, writeSmallArtifact(t), nil)
	if err == nil {
		t.Fatal("expected the completion failure")
	}
	assertNoPresignedSecrets(t, err.Error())
	if strings.Contains(err.Error(), "StringToSign") || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestModelUploadErrorSnippet(t *testing.T) {
	cases := []struct {
		name, body, want string
		notWant          []string
	}{
		{"empty", "", "", nil},
		{"code only", "<Error><Code>AccessDenied</Code></Error>", "AccessDenied", nil},
		{"plain text echoing a URL", "bad request for https://h.example/k" + testPresignedQuery,
			"bad request for https://h.example/k", []string{"deadbeef", "AKIAEXAMPLE"}},
		{"truncated mid-signature", "see https://h.example/k?X-Amz-Signature=deadbeef01", "see https://h.example/k", []string{"deadbeef"}},
		{"unterminated xml", "<Error><Code>Slow</Code><Message>cut off", "Slow", nil},
		{"namespaced code beside an unrecognized element",
			`<Code xmlns="urn:x">AccessDenied</Code><SignatureProvided>deadbeef</SignatureProvided>`,
			"AccessDenied", []string{"deadbeef", "SignatureProvided"}},
		{"namespaced error document", `<?xml version="1.0"?><Error xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Code>SignatureDoesNotMatch</Code><Message>bad signature</Message><AWSAccessKeyId>AKIAEXAMPLE</AWSAccessKeyId><StringToSign>deadbeef</StringToSign></Error>`,
			"SignatureDoesNotMatch: bad signature", []string{"deadbeef", "AKIAEXAMPLE", "StringToSign"}},
		{"xml without code or message", `<Error><SignatureProvided>deadbeef</SignatureProvided></Error>`,
			unrecognizedXMLErrorText, []string{"deadbeef", "SignatureProvided"}},
		{"xml behind a plain-text prefix", `oops <Error><StringToSign>deadbeef</StringToSign></Error>`,
			unrecognizedXMLErrorText, []string{"deadbeef"}},
		{"malformed xml without code or message", `<Error><StringToSign>deadbeef</Str`,
			unrecognizedXMLErrorText, []string{"deadbeef"}},
		{"code text carrying a signed url", `<Error><Code>Bad</Code><Message>see https://h.example/k` + strings.ReplaceAll(testPresignedQuery, "&", "&amp;") + `</Message></Error>`,
			"Bad: see https://h.example/k", []string{"deadbeef", "AKIAEXAMPLE"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := modelUploadErrorSnippet(strings.NewReader(tc.body))
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("got %q, want prefix %q", got, tc.want)
			}
			for _, n := range tc.notWant {
				if strings.Contains(got, n) {
					t.Fatalf("got %q, must not contain %q", got, n)
				}
			}
		})
	}

	t.Run("bounded", func(t *testing.T) {
		got := modelUploadErrorSnippet(strings.NewReader(strings.Repeat("x", 10000)))
		if len([]rune(got)) > modelUploadErrorExcerptRunes+3 {
			t.Fatalf("excerpt not bounded: %d runes", len([]rune(got)))
		}
	})
}

// TestModelUploadErrorSnippetKeyStraddlingTheReadLimit drives the production
// path: the body is cut at modelUploadErrorBodyLimit bytes before any redaction,
// so a configured key (one without an rpa_ prefix) that straddles the limit
// reaches redaction as a bare prefix. No prefix of it may survive, including
// when an earlier long URL shrinks during redaction and pulls that prefix inside
// the excerpt bound.
func TestModelUploadErrorSnippetKeyStraddlingTheReadLimit(t *testing.T) {
	const key = "cfgkey-0123456789-abcdef-ZZ"
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv(configenv.APIKeyEnv, key)

	// the padding before the key is either plain filler or one long url whose
	// query redaction drops, shrinking the text so the cut-off key prefix moves
	// inside the 256-rune excerpt.
	longURL := func(n int) string {
		const base = "see https://h.example/k?"
		return base + strings.Repeat("q", n-len(base)-1) + " "
	}
	plain := func(n int) string { return strings.Repeat("x", n) }
	cases := map[string]func(n int) string{
		"plain filler": plain,
		"long url":     longURL,
	}
	for name, pad := range cases {
		for _, inside := range []int{1, 5, len(key) / 2, len(key) - 1} {
			t.Run(fmt.Sprintf("%s/%d bytes before the limit", name, inside), func(t *testing.T) {
				// place the key so exactly `inside` of its bytes fall before the limit.
				body := pad(modelUploadErrorBodyLimit-inside) + key + " and more"
				if idx := strings.Index(body, key); idx+inside != modelUploadErrorBodyLimit {
					t.Fatalf("test premise: key at %d, want %d bytes before the limit", idx, inside)
				}
				got := modelUploadErrorSnippet(strings.NewReader(body))
				for n := 4; n < len(key); n++ {
					if strings.Contains(got, key[:n]) {
						t.Fatalf("output keeps the key prefix %q: %q", key[:n], got)
					}
				}
				if name == "long url" && !strings.HasPrefix(got, "see https://h.example/k ") {
					t.Fatalf("premise: the long url should have been shortened, got %q", got)
				}
			})
		}
	}
}
