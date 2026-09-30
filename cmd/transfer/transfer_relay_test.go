package transfer

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRelayFetchDefaultTimeoutIsShort guards against regressing to the old
// 2-minute timeout: getRelays runs on every send/receive, so a slow or blocked
// GitHub must not stall the command for minutes before any transfer starts.
func TestRelayFetchDefaultTimeoutIsShort(t *testing.T) {
	if relayFetchTimeout > 15*time.Second {
		t.Fatalf("relay fetch timeout is %s; keep it short (<=15s) so a blocked GitHub does not stall transfers", relayFetchTimeout)
	}
}

// TestGetRelaysFailsFastWhenServerHangs proves the timeout is actually applied:
// against a server that never responds, getRelays returns an error promptly
// rather than blocking for the full timeout budget of a real transfer.
func TestGetRelaysFailsFastWhenServerHangs(t *testing.T) {
	// respond far later than the (tiny) timeout below, so getRelays must give up
	// first. A finite delay avoids blocking server.Close at teardown.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	origURL, origTimeout := relayURL, relayFetchTimeout
	relayURL = server.URL
	relayFetchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { relayURL, relayFetchTimeout = origURL, origTimeout })

	done := make(chan error, 1)
	go func() { _, err := getRelays(); done <- err }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error when the relay server hangs")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("getRelays did not honor its timeout (still blocked after 2s)")
	}
}
