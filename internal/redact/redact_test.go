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
	var chainURLErr *url.Error
	if !errors.As(direct, &chainURLErr) { //nolint:modernize // errors.As, not AsType: nix errcheck flags AsType
		t.Fatal("expected a *url.Error in the chain")
	}
	// the typed error reachable from the result must not carry the signature either.
	for e := direct; e != nil; e = errors.Unwrap(e) {
		var u *url.Error
		if errors.As(e, &u) { //nolint:modernize // errors.As, not AsType: nix errcheck flags AsType
			if strings.Contains(u.URL, "deadbeef") || strings.Contains(u.Error(), "deadbeef") {
				t.Fatalf("typed url.Error still carries the signature: %s", u.URL)
			}
		}
	}
	if !errors.Is(direct, cause) {
		t.Fatal("redaction must keep the error chain")
	}
}

// assertNoSignedURLInChain fails if any node of err's chain, found by walking
// Unwrap() error and Unwrap() []error by hand and through errors.As, still
// exposes the signature as a *url.Error.
func assertNoSignedURLInChain(t *testing.T, err error) int {
	t.Helper()
	seen := 0
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if u, ok := e.(*url.Error); ok { //nolint:errorlint // walking the chain by hand
			seen++
			if strings.Contains(u.URL, "deadbeef") || strings.Contains(u.Error(), "deadbeef") {
				t.Fatalf("typed url.Error still carries the signature: %s", u.URL)
			}
		}
		switch w := e.(type) { //nolint:errorlint // walking the chain by hand
		case interface{ Unwrap() error }:
			walk(w.Unwrap())
		case interface{ Unwrap() []error }:
			for _, c := range w.Unwrap() {
				walk(c)
			}
		}
	}
	walk(err)
	var u *url.Error
	if errors.As(err, &u) { //nolint:modernize // errors.As, not AsType: nix errcheck flags AsType
		if strings.Contains(u.URL, "deadbeef") || strings.Contains(u.Error(), "deadbeef") {
			t.Fatalf("errors.As recovered the signed URL: %s", u.URL)
		}
	}
	return seen
}

type customWrapError struct{ err error }

func (e *customWrapError) Error() string { return "custom: " + e.err.Error() }
func (e *customWrapError) Unwrap() error { return e.err }

func TestErrorSanitizesURLErrorBehindWrappers(t *testing.T) {
	cause := errors.New("connection refused")
	orig := &url.Error{Op: "Put", URL: presigned, Err: cause}
	other := errors.New("other failure")

	cases := map[string]error{
		"fmt wrapped":         fmt.Errorf("upload part 1: %w", orig),
		"double wrapped":      fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", orig)),
		"custom wrapper":      &customWrapError{err: orig},
		"errors.Join":         errors.Join(other, orig),
		"multi-%w":            fmt.Errorf("two: %w and %w", other, orig),
		"joined then wrapped": fmt.Errorf("ctx: %w", errors.Join(other, fmt.Errorf("deep: %w", orig))),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(in.Error(), "deadbeef") {
				t.Fatal("test premise: the input must quote the signature")
			}
			got := Error(in)
			assertClean(t, got.Error())
			if n := assertNoSignedURLInChain(t, got); n == 0 {
				t.Fatal("the typed url.Error should stay reachable (with a clean URL)")
			}
			if !errors.Is(got, cause) {
				t.Fatal("errors.Is on the cause must keep working")
			}
			var u *url.Error
			if !errors.As(got, &u) || u.URL != "https://bucket.s3.us-east-1.amazonaws.com/models/key.bin" {
				t.Fatalf("errors.As should return the sanitized url.Error, got %+v", u)
			}
		})
	}

	t.Run("sibling cause survives errors.Is", func(t *testing.T) {
		got := Error(errors.Join(other, orig))
		if !errors.Is(got, other) || !errors.Is(got, cause) {
			t.Fatal("every cause of a joined error must stay reachable")
		}
	})

	t.Run("chain without a url.Error is kept as is", func(t *testing.T) {
		in := fmt.Errorf("wrap: %w", other)
		got := Error(in)
		if !errors.Is(errors.Unwrap(got), in) {
			t.Fatal("a chain with no url.Error should not be rebuilt")
		}
	})
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
