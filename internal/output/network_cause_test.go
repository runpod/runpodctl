package output

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"

	internalapi "github.com/runpod/runpodctl/internal/api"
)

// a received error status whose body dies with a connection reset reports its
// status and stable code, and still classifies as a transient network failure:
// APIError.Unwrap keeps the (redacted) cause reachable for isNetworkError.
func TestUnreadableBodyErrorOnRejectedStatusStillClassifiesAsNetwork(t *testing.T) {
	readErr := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("read: connection reset by peer")}
	_, err := internalapi.ReadBodyForStatus(http.StatusBadGateway, resetReader{readErr})
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *internalapi.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway || apiErr.ErrorCode() != "server_error" {
		t.Fatalf("expected the received status and code to win, got %v", err)
	}
	if !isNetworkError(err) {
		t.Fatalf("the reset body must still classify as a network error, got %v", err)
	}
}

type resetReader struct{ err error }

func (r resetReader) Read([]byte) (int, error) { return 0, r.err }

// the emitted JSON keeps the received status's code even when the wrapped
// cause is a network error: ErrorCode() wins over the network fallback (the
// published code vocabulary for agents is unchanged by the preserved cause,
// which stays reachable through errors.As for classifiers).
func TestErrorOutputKeepsHTTPCodeOverNetworkCause(t *testing.T) {
	readErr := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("read: connection reset by peer")}
	_, err := internalapi.ReadBodyForStatus(http.StatusBadGateway, resetReader{readErr})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !isNetworkError(err) {
		t.Fatal("premise: the cause must classify as a network error")
	}

	out := captureStderr(t, func() { Error(err) })
	var obj errorObject
	if jsonErr := json.Unmarshal([]byte(out), &obj); jsonErr != nil {
		t.Fatalf("decode error object: %v\n%s", jsonErr, out)
	}
	if obj.Code != "server_error" || obj.Status != http.StatusBadGateway {
		t.Fatalf("expected the http-derived code and status, got %s", out)
	}
}
