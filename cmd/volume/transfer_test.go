package volume

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/runpod/runpodctl/internal/nvtransfer"
)

func s3Status(code int) error {
	return fmt.Errorf("operation failed: %w", &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: code}},
		Err:      fmt.Errorf("http %d", code),
	}})
}

func s3Code(code string, status int) error {
	return fmt.Errorf("operation failed: %w", &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
		Err:      &smithy.GenericAPIError{Code: code},
	}})
}

// a data center whose endpoint does not resolve fails before any transfer;
// any other lookup failure, or an endpoint behind a proxy, is left to the
// transfer.
func TestCheckEndpointResolves(t *testing.T) {
	oldLookup, oldProxy := lookupHost, proxyFor
	t.Cleanup(func() { lookupHost, proxyFor = oldLookup, oldProxy })
	proxyFor = func(*http.Request) (*url.URL, error) { return nil, nil }
	lookupResult := error(nil)
	lookupHost = func(context.Context, string) ([]string, error) { return nil, lookupResult }

	lookupResult = &net.DNSError{Name: "s3api-nope-1.runpod.io", IsNotFound: true}
	err := checkEndpointResolves("https://s3api-nope-1.runpod.io/")
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) || coder.ErrorCode() != "usage_error" || !strings.Contains(err.Error(), "--data-center-id") {
		t.Fatalf("expected usage_error naming --data-center-id, got %v", err)
	}

	lookupResult = &net.DNSError{Name: "s3api-eu-ro-1.runpod.io", IsTemporary: true}
	if err := checkEndpointResolves("https://s3api-eu-ro-1.runpod.io/"); err != nil {
		t.Fatalf("a failure other than not-found must be left to the retries: %v", err)
	}

	lookupResult = &net.DNSError{Name: "s3api-eu-ro-1.runpod.io", IsNotFound: true}
	proxyFor = func(*http.Request) (*url.URL, error) { return url.Parse("http://proxy.example:3128") }
	if err := checkEndpointResolves("https://s3api-eu-ro-1.runpod.io/"); err != nil {
		t.Fatalf("behind a proxy the local lookup must be skipped: %v", err)
	}
}

func TestTransferChecksEndpointBeforeS3Keys(t *testing.T) {
	oldDC, oldEndpoint, oldAccess, oldSecret, oldLookup, oldProxy := transferDataCenterID, transferEndpointURL, transferAccessKey, transferSecretKey, lookupHost, proxyFor
	t.Cleanup(func() {
		transferDataCenterID, transferEndpointURL, transferAccessKey, transferSecretKey, lookupHost, proxyFor = oldDC, oldEndpoint, oldAccess, oldSecret, oldLookup, oldProxy
	})
	transferDataCenterID, transferEndpointURL = "NOPE-1", ""
	transferAccessKey, transferSecretKey = "", ""
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNPOD_S3_ACCESS_KEY", "")
	t.Setenv("RUNPOD_S3_SECRET_KEY", "")
	proxyFor = func(*http.Request) (*url.URL, error) { return nil, nil }
	lookupHost = func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Name: "s3api-nope-1.runpod.io", IsNotFound: true}
	}
	err := runTransfer(nil, "vol1", "src", "dst", nil)
	if err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("expected endpoint error before credential check, got %v", err)
	}
}

// transferError is the contract agents branch on: only a failure a rerun can
// get past is transfer_incomplete with the resume hint.
func TestTransferErrorCodes(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name   string
		ctx    context.Context
		err    error
		code   string
		status int
		resume bool
	}{
		{"interrupted", canceled, errors.New("boom"), "transfer_incomplete", 0, true},
		{"invalid path", nil, fmt.Errorf("bad: %w", nvtransfer.ErrInvalidPath), "usage_error", 0, false},
		{"missing path", nil, fmt.Errorf("x: %w", nvtransfer.ErrNotFound), "not_found", 0, false},
		{"refused", nil, fmt.Errorf("upload x: %w", &nvtransfer.RefusedError{Err: errors.New("too large")}), "cli_error", 0, false},
		{"dns drop mid-transfer", nil, &net.DNSError{Name: "s3api-eu-ro-1.runpod.io", IsNotFound: true}, "transfer_incomplete", 0, true},
		{"upload vanished", nil, s3Code("NoSuchUpload", 404), "transfer_incomplete", 0, true},
		{"range no longer fits", nil, s3Status(416), "transfer_incomplete", 0, true},
		{"rename blocked", nil, &os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EISDIR}, "cli_error", 0, false},
		{"unauthorized", nil, s3Status(401), "unauthorized", 401, false},
		{"forbidden", nil, s3Status(403), "forbidden", 403, false},
		{"no such bucket", nil, s3Status(404), "not_found", 404, false},
		{"bad request", nil, s3Status(400), "bad_request", 400, false},
		{"rate limited", nil, s3Status(429), "transfer_incomplete", 0, true},
		{"server error", nil, s3Status(503), "transfer_incomplete", 0, true},
		{"full disk", nil, &fs.PathError{Op: "write", Path: "out", Err: syscall.ENOSPC}, "cli_error", 0, false},
		{"network", nil, errors.New("connection reset"), "transfer_incomplete", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := c.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			err := transferError(ctx, c.err, nil)
			var coder interface{ ErrorCode() string }
			if !errors.As(err, &coder) || coder.ErrorCode() != c.code {
				t.Fatalf("code = %v, want %s (%v)", coder, c.code, err)
			}
			var statuser interface{ HTTPStatus() int }
			got := 0
			if errors.As(err, &statuser) {
				got = statuser.HTTPStatus()
			}
			if got != c.status {
				t.Errorf("status = %d, want %d", got, c.status)
			}
			if hint := strings.Contains(err.Error(), "re-run the same command"); hint != c.resume {
				t.Errorf("resume hint = %v, want %v: %s", hint, c.resume, err)
			}
		})
	}
}

func TestRejectedKeysNameTheFix(t *testing.T) {
	for _, status := range []int{401, 403} {
		msg := transferError(context.Background(), s3Status(status), nil).Error()
		if !strings.HasPrefix(msg, "the volume's s3 api rejected the keys") ||
			!strings.Contains(msg, "RUNPOD_S3_ACCESS_KEY") ||
			!strings.Contains(msg, "~/.runpod/config.toml") ||
			!strings.Contains(msg, "http ") {
			t.Errorf("%d: %s", status, msg)
		}
	}
}

func TestRejectedAPIKeyNamesTheFix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Subject token is invalid or expired"}`))
	}))
	defer server.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNPOD_API_KEY", "rpa_invalid")
	t.Setenv("RUNPOD_REST_V2_URL", server.URL+"/v2/")

	_, err := lookupDataCenter("vol1")
	if !errors.Is(err, errAPIKeyRejected) {
		t.Fatalf("expected the rejected-key error, got %v", err)
	}
	if !strings.Contains(err.Error(), "~/.runpod/config.toml") || !strings.Contains(err.Error(), "Subject token is invalid or expired") {
		t.Fatalf("expected saved-key and API error details, got %v", err)
	}
}
