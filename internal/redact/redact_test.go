package redact

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
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
		// network-path references keep no userinfo, query or fragment and stay scheme-relative.
		"//user:pw@example.test/x":                             "//example.test/x",
		"//user@example.test/x?token=abc#frag":                 "//example.test/x",
		"//user:pw@example.test:8443/a/b?token=abc":            "//example.test:8443/a/b",
		"https://user:pw@host/x?X-Amz-Signature=deadbeef":      "https://host/x",
		"//example.test":                                       "//example.test",
		"/path?token=abc":                                      "/path",
		"/path?token=abc#frag":                                 "/path",
		"relative/path#frag":                                   "relative/path",
		"//user:pw@exa mple.test/x?token=abc":                  "//exa mple.test/x",
		"//user:pw@example.test:badport/x?token=abc#frag":      "//example.test:badport/x",
		"https://user:pw@exa mple.test/x?X-Amz-Signature=beef": "https://exa mple.test/x",
	}
	for in, want := range cases {
		got := URL(in)
		if got != want {
			t.Errorf("URL(%q) = %q, want %q", in, got, want)
		}
		for _, secret := range []string{"pw", "deadbeef", "abc", "user:", "frag", "beef"} {
			if strings.Contains(got, secret) {
				t.Errorf("URL(%q) = %q still contains %q", in, got, secret)
			}
		}
	}
}

// A network-path url.Error (what net/http reports for a refused redirect to
// "//user:pw@host/...") must lose its userinfo and query in every rendering and
// in the typed URL field.
func TestErrorScrubsNetworkPathURLError(t *testing.T) {
	for _, raw := range []string{
		"//user:pw@example.test/x",
		"//user@example.test/x?token=abc#frag",
		"https://user:pw@host/x?X-Amz-Signature=deadbeef",
		"/path?token=abc",
	} {
		orig := &url.Error{Op: "Post", URL: raw, Err: errors.New("refused")}
		got := Error(fmt.Errorf("request failed: %w", orig))
		var ue *url.Error
		if !errors.As(got, &ue) {
			t.Fatalf("%q: no *url.Error in chain", raw)
		}
		for how, text := range map[string]string{
			"Error()": got.Error(), "%v": fmt.Sprintf("%v", got), "%+v": fmt.Sprintf("%+v", got), "URL": ue.URL,
		} {
			for _, secret := range []string{"pw", "deadbeef", "abc", "user:", "user@", "frag"} {
				if strings.Contains(text, secret) {
					t.Errorf("%q: %s contains %q: %s", raw, how, secret, text)
				}
			}
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

// timeoutNetError is a net.Error whose Timeout answer is set by the test.
type timeoutNetError struct{ timeout bool }

func (e *timeoutNetError) Error() string   { return "i/o timeout" }
func (e *timeoutNetError) Timeout() bool   { return e.timeout }
func (e *timeoutNetError) Temporary() bool { return false }

// Sanitizing must not change how a failure classifies: a timeout stays a
// timeout (errors.As to net.Error, Timeout(), errors.Is on the deadline
// sentinels) while the URL is scrubbed. Temporary() is deprecated (SA1019)
// and deliberately not asserted.
func TestErrorKeepsTimeoutClassification(t *testing.T) {
	const rawURL = "https://user:pw@host.test/x?X-Amz-Signature=deadbeef" //nolint:gosec // G101: fake credential fixture
	scrubbed := []string{"pw", "deadbeef", "user:", "X-Amz-Signature"}

	wrapped := func(cause error) error {
		return &url.Error{Op: "Get", URL: rawURL, Err: cause}
	}
	cases := []struct {
		name    string
		err     error
		timeout bool
	}{
		{"net timeout", wrapped(&timeoutNetError{timeout: true}), true},
		{"net non-timeout", wrapped(&timeoutNetError{}), false},
		{"context deadline", wrapped(context.DeadlineExceeded), true},
		{"os deadline", wrapped(os.ErrDeadlineExceeded), true},
		{"op error timeout", wrapped(&net.OpError{Op: "dial", Net: "tcp", Err: &timeoutNetError{timeout: true}}), true},
		{"plain refusal", wrapped(errors.New("connection refused")), false},
		{"behind fmt.Errorf", fmt.Errorf("request failed: %w", wrapped(&timeoutNetError{timeout: true})), true},
		{"behind errors.Join", errors.Join(errors.New("other"), fmt.Errorf("x: %w", wrapped(context.DeadlineExceeded))), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Error(tc.err)

			for _, secret := range scrubbed {
				if strings.Contains(got.Error(), secret) {
					t.Errorf("Error() contains %q: %s", secret, got)
				}
			}
			var ue *url.Error
			if !errors.As(got, &ue) {
				t.Fatal("no *url.Error in chain")
			}
			for _, secret := range scrubbed {
				if strings.Contains(ue.URL, secret) {
					t.Errorf("url.Error.URL contains %q: %s", secret, ue.URL)
				}
			}
			if ue.Timeout() != tc.timeout {
				t.Errorf("url.Error Timeout = %v, want %v", ue.Timeout(), tc.timeout)
			}

			var netErr net.Error
			if !errors.As(got, &netErr) {
				t.Fatal("errors.As to net.Error failed")
			}
			if netErr.Timeout() != tc.timeout {
				t.Errorf("net.Error Timeout = %v, want %v", netErr.Timeout(), tc.timeout)
			}
			// the sentinels answer exactly as they did before sanitizing.
			for _, sentinel := range []error{context.DeadlineExceeded, os.ErrDeadlineExceeded} {
				if errors.Is(got, sentinel) != errors.Is(tc.err, sentinel) {
					t.Errorf("errors.Is(%v) = %v, want %v", sentinel, errors.Is(got, sentinel), errors.Is(tc.err, sentinel))
				}
			}
			// idempotent: redacting a redacted error keeps the classification.
			again := Error(got)
			if !errors.As(again, &netErr) || netErr.Timeout() != tc.timeout {
				t.Errorf("classification lost on second Error()")
			}
		})
	}
}

// An error chain with nothing network-shaped in it must not start to look like
// a net.Error after sanitizing.
func TestErrorWrapperIsNotANetErrorWithoutOne(t *testing.T) {
	wrapped := fmt.Errorf("custom: %w", &customWrapError{err: &url.Error{Op: "Get", URL: "https://u:pw@h/x", Err: errors.New("boom")}}) //nolint:gosec // G101: fake credential fixture
	got := Error(errors.New("plain, no url"))
	var netErr net.Error
	if errors.As(got, &netErr) {
		t.Errorf("plain error became a net.Error: %v", netErr)
	}
	// a url.Error is a net.Error already; its answer must stay "not a timeout".
	if errors.As(Error(wrapped), &netErr) && netErr.Timeout() {
		t.Error("non-timeout url.Error reports Timeout()")
	}
}

// A real client timeout, as net/http produces it, still classifies as a timeout
// after the request URL has been scrubbed.
func TestErrorKeepsRealClientTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })

	target := strings.Replace(srv.URL, "http://", "http://user:pw@", 1) + "/x?token=abc&X-Amz-Signature=deadbeef"
	_, err := (&http.Client{Timeout: 50 * time.Millisecond}).Get(target) //nolint:noctx,bodyclose // the request fails by design
	if err == nil {
		t.Fatal("expected a timeout")
	}
	got := Error(fmt.Errorf("request failed: %w", err))

	var netErr net.Error
	if !errors.As(got, &netErr) || !netErr.Timeout() {
		t.Fatalf("timeout classification lost: %v", got)
	}
	var ue *url.Error
	if !errors.As(got, &ue) || !ue.Timeout() {
		t.Fatalf("url.Error no longer reports Timeout(): %v", got)
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("errors.Is(DeadlineExceeded) lost: %v", got)
	}
	for _, text := range []string{got.Error(), ue.URL} {
		for _, secret := range []string{"pw", "abc", "deadbeef", "user"} {
			if strings.Contains(text, secret) {
				t.Errorf("contains %q: %s", secret, text)
			}
		}
	}
}
