package serverless

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
)

// withV2Server points the v2 client at a test server for the duration of a test.
// The api key is set too, since NewV2Client refuses to build without one.
func withV2Server(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Setenv(configenv.RESTV2URLEnv, server.URL)
	t.Setenv(configenv.APIKeyEnv, "test-key")
	// viper is a process global with no per-test scoping, so the previous value is
	// restored rather than left overwritten -- 0 here means "fall back to
	// DefaultTimeout", and leaking that into a test that set its own short timeout
	// would change what that test exercises.
	previousTimeout := viper.Get("timeout")
	viper.Set("timeout", 0)
	t.Cleanup(func() { viper.Set("timeout", previousTimeout) })
	t.Cleanup(server.Close)
	return server
}

func TestLogsCmdRegistered(t *testing.T) {
	found := false
	for _, cmd := range Cmd.Commands() {
		if cmd.Use == "logs <endpoint-id>" {
			found = true
		}
	}
	if !found {
		t.Error("serverless logs is not registered on the serverless command")
	}
}

// An explicit --worker must not cost an api call: a worker id from any source
// (including one the listing no longer reports) still has readable logs.
func TestResolveLogTargetsExplicitWorkerSkipsAPI(t *testing.T) {
	called := false
	withV2Server(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	})

	targets, err := resolveLogTargets("ep1", "w1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if called {
		t.Error("listed workers even though --worker was given")
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	if targets[0].WorkerID != "w1" {
		t.Errorf("workerId = %q, want w1", targets[0].WorkerID)
	}
	if targets[0].Path != "/serverless/ep1/workers/w1/logs" {
		t.Errorf("path = %q", targets[0].Path)
	}
}

// Without --worker every worker is read, since the caller usually does not know
// which one is broken -- that is the whole problem being solved.
func TestResolveLogTargetsFansOutOverWorkers(t *testing.T) {
	withV2Server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/serverless/ep1/workers" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		// the shape prod returns, captured live.
		fmt.Fprint(w, `{"endpointVersion":1,"summary":{"total":2,"unhealthy":1},"workers":[
			{"id":"w1","status":"UNHEALTHY"},{"id":"w2","status":"IDLE"}]}`)
	})

	targets, err := resolveLogTargets("ep1", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
	for i, want := range []string{"w1", "w2"} {
		if targets[i].WorkerID != want {
			t.Errorf("target %d workerId = %q, want %q", i, targets[i].WorkerID, want)
		}
	}
}

// An endpoint with no workers has no logs, but that is not an api failure and
// must not read as "no output". It reports `conflict` -- the endpoint exists and
// the id was right, it is just in a state with nothing to stream. Reporting
// usage_error here would tell an agent its input was wrong, so it would re-guess
// the endpoint id instead of following the advice in the message.
func TestResolveLogTargetsNoWorkersIsConflict(t *testing.T) {
	withV2Server(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"summary":{"total":0},"workers":[]}`)
	})

	_, err := resolveLogTargets("ep1", "")
	if err == nil {
		t.Fatal("expected an error for an endpoint with no workers")
	}
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) || coder.ErrorCode() != "conflict" {
		t.Errorf("err = %v, want code conflict", err)
	}
	// the message has to name the actionable next step, not just the state.
	if !strings.Contains(err.Error(), "serverless health") {
		t.Errorf("message should point at the follow-up command: %q", err)
	}
}

// A worker entry with no id cannot be addressed, so it must be skipped rather
// than turned into a request against /workers//logs.
func TestResolveLogTargetsSkipsWorkersWithoutIDs(t *testing.T) {
	withV2Server(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"workers":[{"id":"","status":"IDLE"},{"id":"w2","status":"IDLE"}]}`)
	})

	targets, err := resolveLogTargets("ep1", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(targets) != 1 || targets[0].WorkerID != "w2" {
		t.Fatalf("targets = %+v, want only w2", targets)
	}
}

func TestResolveLogTargetsPropagatesAPIError(t *testing.T) {
	withV2Server(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"detail":"endpoint not found","status":404}`)
	})

	_, err := resolveLogTargets("nope", "")
	if err == nil {
		t.Fatal("expected the 404 to propagate")
	}
	var coder interface{ ErrorCode() string }
	if !errors.As(err, &coder) || coder.ErrorCode() != "not_found" {
		t.Errorf("err = %v, want code not_found", err)
	}
}
