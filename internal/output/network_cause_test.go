package output

import (
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
