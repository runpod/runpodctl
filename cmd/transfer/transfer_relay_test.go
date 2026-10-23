package transfer

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// TestRelayFetchDefaultTimeoutIsShort guards against regressing to the old
// 2-minute timeout: getRelays runs on every send/receive, so a slow or blocked
// GitHub must not stall the command for minutes before any transfer starts.
// Zero is rejected too, since http.Client treats it as "no timeout".
func TestRelayFetchDefaultTimeoutIsShort(t *testing.T) {
	if relayFetchTimeout <= 0 || relayFetchTimeout > 15*time.Second {
		t.Fatalf("relay fetch timeout is %s; want 0 < timeout <= 15s so a blocked GitHub does not stall transfers", relayFetchTimeout)
	}
}

// TestGetRelaysTimesOutOnBlockedServer proves the client timeout is applied:
// the handler stays pending until the request is canceled, so only the
// client-side timeout can produce a prompt *url.Error.
func TestGetRelaysTimesOutOnBlockedServer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // runs before srv.Close

	origURL, origTimeout := relayURL, relayFetchTimeout
	relayURL, relayFetchTimeout = srv.URL, 50*time.Millisecond
	t.Cleanup(func() { relayURL, relayFetchTimeout = origURL, origTimeout })

	done := make(chan error, 1)
	go func() { _, err := getRelays(); done <- err }()

	select {
	case err := <-done:
		var uerr *url.Error
		if !errors.As(err, &uerr) || !uerr.Timeout() {
			t.Fatalf("want client timeout error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("getRelays ignored relayFetchTimeout")
	}
}
