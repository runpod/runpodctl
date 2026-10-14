package pods

import (
	"errors"
	"fmt"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

// --gpuType "A,B" moves on to B only when A was cleanly refused; a 5xx or a
// timeout may have created the pod
func TestNoCapacity(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&api.APIError{Status: 409, Message: "no capacity"}, true},
		{fmt.Errorf("wrapped: %w", &api.APIError{Status: 400, Message: "x"}), true},
		{&api.APIError{Status: 502, Message: "bad gateway"}, false},
		{errors.New("request failed: timeout"), false},
		{errors.New("There are no longer any instances available with the requested specifications"), true},
	}
	for _, tc := range cases {
		if got := noCapacity(tc.err); got != tc.want {
			t.Errorf("noCapacity(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
