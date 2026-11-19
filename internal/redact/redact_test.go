package redact

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const presigned = "https://bucket.s3.us-east-1.amazonaws.com/models/key.bin?partNumber=1&uploadId=U1" +
	"&X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAEXAMPLE%2F20260101%2Fus-east-1%2Fs3%2Faws4_request" +
	"&X-Amz-Date=20260101T000000Z&X-Amz-Expires=900&X-Amz-Security-Token=TOKENVALUE&X-Amz-SignedHeaders=host" +
	"&X-Amz-Signature=deadbeef0123456789"

var secrets = []string{"deadbeef0123456789", "AKIAEXAMPLE", "TOKENVALUE", "uploadId", "X-Amz-Signature=deadbeef"}

func assertClean(t *testing.T, got string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(got, secret) {
			t.Fatalf("output still contains %q: %s", secret, got)
		}
	}
}

func TestURLStripsQueryUserinfoFragment(t *testing.T) {
	cases := map[string]string{ //nolint:gosec // G101: fake presigned-URL fixtures, not credentials
		presigned:                                "https://bucket.s3.us-east-1.amazonaws.com/models/key.bin",
		"https://user:pw@host.example/a/b?x=1#f": "https://host.example/a/b",
		"https://host.example/plain":             "https://host.example/plain",
		"host.example/p?X-Amz-Signature=abc":     "host.example/p",
	}
	for in, want := range cases {
		if got := URL(in); got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTextRedactsURLsAndParams(t *testing.T) {
	cases := map[string]string{ //nolint:gosec // G101: fake presigned-URL fixtures, not credentials
		"Put \"" + presigned + "\": dial tcp 127.0.0.1:1: connect: connection refused": "Put \"https://bucket.s3.us-east-1.amazonaws.com/models/key.bin\": dial tcp 127.0.0.1:1: connect: connection refused",
		// JSON with \u0026 separators must not leave the query tail behind.
		`{"url":"https://h.example/k?a=1\u0026X-Amz-Signature=deadbeef0123456789"}`: `{"url":"https://h.example/k"}`,
		// body cut off mid-URL / scheme-less: the parameter value goes, the name stays.
		"truncated ...?X-Amz-Credential=AKIAEXAMPLE%2Fx&X-Amz-Signature=deadbeef0123456789": "truncated ...?X-Amz-Credential=REDACTED&X-Amz-Signature=REDACTED",
		"no secrets here": "no secrets here",
	}
	for in, want := range cases {
		got := Text(in)
		if got != want {
			t.Errorf("Text(%q)\n got %q\nwant %q", in, got, want)
		}
		assertClean(t, got)
	}
}

func TestErrorRewritesURLError(t *testing.T) {
	cause := errors.New("connection refused")
	orig := &url.Error{Op: "Put", URL: presigned, Err: cause}
	if !strings.Contains(orig.Error(), "deadbeef") {
		t.Fatal("test premise: url.Error should quote the signature")
	}

	got := Error(fmt.Errorf("upload part 1: %w", orig))
	assertClean(t, got.Error())
	if !strings.Contains(got.Error(), "https://bucket.s3.us-east-1.amazonaws.com/models/key.bin") ||
		!strings.Contains(got.Error(), "connection refused") {
		t.Fatalf("host/path and cause should survive: %s", got)
	}
	if !errors.Is(got, cause) {
		t.Fatal("redaction must keep the error chain")
	}

	direct := Error(orig)
	if _, ok := errors.AsType[*url.Error](direct); !ok {
		t.Fatal("expected a *url.Error in the chain")
	}
	// the typed error reachable from the result must not carry the signature either.
	for e := direct; e != nil; e = errors.Unwrap(e) {
		if u, ok := errors.AsType[*url.Error](e); ok {
			if strings.Contains(u.URL, "deadbeef") || strings.Contains(u.Error(), "deadbeef") {
				t.Fatalf("typed url.Error still carries the signature: %s", u.URL)
			}
		}
	}
	if !errors.Is(direct, cause) {
		t.Fatal("redaction must keep the error chain")
	}
}

func TestErrorFromRealRequestFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presigned, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, doErr := http.DefaultClient.Do(req)
	if doErr == nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close: %v", closeErr)
		}
		t.Fatal("expected an error")
	}
	got := Error(doErr)
	assertClean(t, got.Error())
	if !errors.Is(got, context.Canceled) {
		t.Fatal("errors.Is(context.Canceled) must still hold")
	}
}

func TestErrorFromParseFailure(t *testing.T) {
	_, err := http.NewRequestWithContext(context.Background(), http.MethodPut, "https://bad host/k?X-Amz-Signature=deadbeef0123456789", http.NoBody)
	if err == nil {
		t.Skip("URL unexpectedly parsed")
	}
	assertClean(t, Error(err).Error())
}

func TestErrorNilAndIdempotent(t *testing.T) {
	if Error(nil) != nil {
		t.Fatal("Error(nil) must be nil")
	}
	once := Error(errors.New("x " + presigned))
	if !errors.Is(Error(once), once) || Error(once).Error() != once.Error() {
		t.Fatal("redacting twice should be a no-op")
	}
}
