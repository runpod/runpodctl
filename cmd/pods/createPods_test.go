package pods

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

func TestLegacyCreatePodsEnvKeepsCommasAndQuotes(t *testing.T) {
	oldEnv := env
	flag := CreatePodsCmd.Flags().Lookup("env")
	oldChanged := flag.Changed
	t.Cleanup(func() {
		env = oldEnv
		flag.Changed = oldChanged
	})

	if err := CreatePodsCmd.Flags().Parse([]string{
		"--env", "CUDA_VISIBLE_DEVICES=0,1",
		"--env", `GREETING="hi"`,
	}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	want := []string{"CUDA_VISIBLE_DEVICES=0,1", `GREETING="hi"`}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("env = %q, want %q", env, want)
	}
}

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
		// a 5xx whose body carries the capacity wording is still uncertain
		{&api.APIError{Status: 500, Message: "There are no longer any instances available"}, false},
		{errors.New("request failed: timeout"), false},
		{errors.New("There are no longer any instances available with the requested specifications"), true},
		{uncertain{"request failed: There are no longer any instances available"}, false},
	}
	for _, tc := range cases {
		if got := noCapacity(tc.err); got != tc.want {
			t.Errorf("noCapacity(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

type uncertain struct{ msg string }

func (u uncertain) Error() string     { return u.msg }
func (u uncertain) ErrorCode() string { return "create_uncertain" }
