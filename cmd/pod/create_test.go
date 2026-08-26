package pod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/waitfor"
	"github.com/spf13/cobra"
)

func TestParseDockerArgs(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		wantCmd        []string
		wantEntrypoint []string
	}{
		{
			name:    "simple command",
			in:      "sleep infinity",
			wantCmd: []string{"sleep", "infinity"},
		},
		{
			name:    "single token",
			in:      "nginx",
			wantCmd: []string{"nginx"},
		},
		{
			name:    "quoted argument stays one token",
			in:      `bash -c "sleep infinity"`,
			wantCmd: []string{"bash", "-c", "sleep infinity"},
		},
		{
			name:    "single quotes",
			in:      `sh -c 'while true; do date; sleep 60; done'`,
			wantCmd: []string{"sh", "-c", "while true; do date; sleep 60; done"},
		},
		{
			name:    "extra whitespace",
			in:      "  sleep   infinity  ",
			wantCmd: []string{"sleep", "infinity"},
		},
		{
			name:    "unbalanced quote falls back to whitespace split",
			in:      `sleep "infinity`,
			wantCmd: []string{"sleep", `"infinity`},
		},
		{
			name: "whitespace-only input yields no tokens",
			in:   "   ",
		},
		{
			name:           "canonical json object form",
			in:             `{"cmd":["sleep","infinity"],"entrypoint":["/bin/sh","-c"]}`,
			wantCmd:        []string{"sleep", "infinity"},
			wantEntrypoint: []string{"/bin/sh", "-c"},
		},
		{
			name:    "json object with cmd only",
			in:      `{"cmd":["python -u app.py"]}`,
			wantCmd: []string{"python -u app.py"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, entrypoint := parseDockerArgs(tc.in)
			assertTokens(t, "cmd", cmd, tc.wantCmd)
			assertTokens(t, "entrypoint", entrypoint, tc.wantEntrypoint)
		})
	}
}

func assertTokens(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

func TestCreateCmd_WaitFlags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("wait") == nil {
		t.Error("expected --wait flag")
	}
	waitTimeout := flags.Lookup("wait-timeout")
	if waitTimeout == nil {
		t.Fatal("expected --wait-timeout flag")
	}
	if waitTimeout.DefValue != "10m" {
		t.Errorf("--wait-timeout default = %q, want 10m", waitTimeout.DefValue)
	}
}

// snapshotWaitFlags restores the --wait globals after a test mutates them.
func snapshotWaitFlags(t *testing.T) {
	t.Helper()
	oldWait, oldTimeout, oldSSH := createWait, createWaitTimeout, createSSH
	t.Cleanup(func() {
		createWait, createWaitTimeout, createSSH = oldWait, oldTimeout, oldSSH
	})
	createWait, createWaitTimeout, createSSH = false, defaultWaitTimeout, true
}

func waitCommand(changedFlags ...string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{}
	cmd.Flags().String("wait-timeout", defaultWaitTimeout, "")
	for _, name := range changedFlags {
		cmd.Flags().Lookup(name).Changed = true
	}
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	return cmd, stderr
}

// These all run before the pod is created, so an unsatisfiable --wait costs the
// user nothing.
func TestResolveWaitTimeout(t *testing.T) {
	cases := []struct {
		name         string
		setup        func()
		computeType  string
		cloudType    string
		publicIP     bool
		changedFlags []string
		want         time.Duration
		wantErr      string
		wantStderr   string
	}{
		{
			name:        "no wait means no timeout",
			computeType: "GPU",
			want:        0,
		},
		{
			name:         "wait-timeout without wait is called out, not silently dropped",
			computeType:  "GPU",
			changedFlags: []string{"wait-timeout"},
			want:         0,
			wantStderr:   "--wait-timeout has no effect without --wait",
		},
		{
			name:        "default timeout",
			setup:       func() { createWait = true },
			computeType: "GPU",
			want:        10 * time.Minute,
		},
		{
			name:        "explicit timeout, days included",
			setup:       func() { createWait = true; createWaitTimeout = "90s" },
			computeType: "GPU",
			want:        90 * time.Second,
		},
		{
			name:        "unparseable timeout",
			setup:       func() { createWait = true; createWaitTimeout = "later" },
			computeType: "GPU",
			wantErr:     `invalid --wait-timeout: invalid duration "later"`,
		},
		{
			// nothing will ever listen on port 22, so this can only time out.
			name:        "wait with ssh disabled is refused",
			setup:       func() { createWait = true; createSSH = false },
			computeType: "GPU",
			wantErr:     "--wait waits for ssh, so it cannot be combined with --ssh=false",
		},
		{
			// cpu pods go through rest, which cannot request runpod-managed ssh;
			// prod still allocates a public port 22, so this warns instead of
			// refusing outright.
			name:        "cpu warns that ssh depends on the image",
			setup:       func() { createWait = true },
			computeType: "CPU",
			want:        10 * time.Minute,
			wantStderr:  "cpu pods are created through the rest api",
		},
		{
			// no public ip means no publicly mapped port 22 to probe, so this wait
			// can time out for a reason the flags do not make obvious.
			name:        "community cloud without a public ip is called out",
			setup:       func() { createWait = true },
			computeType: "GPU",
			cloudType:   "COMMUNITY",
			want:        10 * time.Minute,
			wantStderr:  "community cloud only maps a public ssh port",
		},
		{
			name:        "community cloud with a public ip is fine",
			setup:       func() { createWait = true },
			computeType: "GPU",
			cloudType:   "COMMUNITY",
			publicIP:    true,
			want:        10 * time.Minute,
		},
		{
			name:        "secure cloud says nothing about public ips",
			setup:       func() { createWait = true },
			computeType: "GPU",
			cloudType:   "SECURE",
			want:        10 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotWaitFlags(t)
			if tc.setup != nil {
				tc.setup()
			}
			cmd, stderr := waitCommand(tc.changedFlags...)

			cloudType := tc.cloudType
			if cloudType == "" {
				cloudType = "SECURE"
			}
			got, err := resolveWaitTimeout(cmd, tc.computeType, cloudType, tc.publicIP)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("timeout = %v, want %v", got, tc.want)
			}
			if tc.wantStderr == "" {
				if stderr.Len() != 0 {
					t.Errorf("unexpected stderr: %q", stderr.String())
				}
			} else if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

// The two create paths return different shapes; --wait has to find the id in
// both, and must not silently poll nothing when it cannot.
func TestPodIDFrom(t *testing.T) {
	cases := []struct {
		name    string
		result  interface{}
		want    string
		wantErr bool
	}{
		{name: "rest pod", result: &api.Pod{ID: "pod-rest"}, want: "pod-rest"},
		{name: "graphql map", result: map[string]interface{}{"id": "pod-gql"}, want: "pod-gql"},
		{name: "rest pod without an id", result: &api.Pod{}, wantErr: true},
		{name: "nil rest pod", result: (*api.Pod)(nil), wantErr: true},
		{name: "graphql map without an id", result: map[string]interface{}{"desiredStatus": "RUNNING"}, wantErr: true},
		{name: "graphql map with a non-string id", result: map[string]interface{}{"id": 7}, wantErr: true},
		{name: "unexpected shape", result: "pod-1", wantErr: true},
		{name: "nil", result: nil, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := podIDFrom(tc.result)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				if !strings.Contains(err.Error(), "runpodctl pod list") {
					t.Errorf("the error must tell the user how to find the pod: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("id = %q, want %q", got, tc.want)
			}
		})
	}
}

type fakePodLister struct {
	pods  []*api.LegacyPod
	calls int
}

func (f *fakePodLister) GetPods() ([]*api.LegacyPod, error) {
	f.calls++
	return f.pods, nil
}

func runningPodWithSSH(id string, public bool) *api.LegacyPod {
	return &api.LegacyPod{
		ID:            id,
		DesiredStatus: "RUNNING",
		Runtime: &api.LegacyRuntime{
			Ports: []*api.LegacyPort{{Ip: "1.2.3.4", IsIpPublic: public, PrivatePort: 22, PublicPort: 51227}},
		},
	}
}

func TestWaitForPodSSH(t *testing.T) {
	oldLister, oldProbe, oldInterval := newPodWaitLister, podSSHProbe, waitPollInterval
	t.Cleanup(func() { newPodWaitLister, podSSHProbe, waitPollInterval = oldLister, oldProbe, oldInterval })
	waitPollInterval = time.Millisecond

	t.Run("returns once ssh answers", func(t *testing.T) {
		lister := &fakePodLister{pods: []*api.LegacyPod{runningPodWithSSH("pod-1", true)}}
		newPodWaitLister = func() (waitfor.PodLister, error) { return lister, nil }
		podSSHProbe = func(context.Context, string) error { return nil }

		cmd, stderr := waitCommand()
		addr, err := waitForPodSSH(context.Background(), cmd, "pod-1", time.Minute)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// the proven address comes back so a failed re-read can still report it.
		if addr != "1.2.3.4:51227" {
			t.Errorf("addr = %q, want 1.2.3.4:51227", addr)
		}
		if lister.calls != 1 {
			t.Errorf("polled %d times, want 1", lister.calls)
		}
		// the success line names the address that answered, so the run is
		// self-evidencing rather than just "ready".
		if !strings.Contains(stderr.String(), "ssh on pod pod-1 ready after 0s: ssh reachable at 1.2.3.4:51227") {
			t.Errorf("progress must go to stderr and name the address: %q", stderr.String())
		}
	})

	t.Run("timeout keeps the pod and names it", func(t *testing.T) {
		lister := &fakePodLister{pods: []*api.LegacyPod{runningPodWithSSH("pod-1", true)}}
		newPodWaitLister = func() (waitfor.PodLister, error) { return lister, nil }
		podSSHProbe = func(context.Context, string) error {
			return errors.New("connect: connection refused")
		}

		cmd, stderr := waitCommand()
		_, err := waitForPodSSH(context.Background(), cmd, "pod-1", 2*time.Millisecond)
		if err == nil {
			t.Fatal("expected a timeout")
		}

		var waitErr *waitfor.Error
		if !errors.As(err, &waitErr) {
			t.Fatalf("expected a *waitfor.Error, got %#v", err)
		}
		if waitErr.ErrorCode() != waitfor.CodeTimeout {
			t.Errorf("code = %q, want %q", waitErr.ErrorCode(), waitfor.CodeTimeout)
		}
		if id := resourceIDOf(err); id != "pod-1" {
			t.Errorf("error id = %q, want pod-1 as machine-readable data", id)
		}
		for _, want := range []string{
			"pod pod-1 was created",
			"runpodctl pod delete pod-1",
			"connection refused",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), want)
			}
		}
		if !strings.Contains(stderr.String(), "waiting for ssh on pod pod-1") {
			t.Errorf("stderr = %q, want the opening progress line", stderr.String())
		}
	})

	t.Run("propagates a client failure", func(t *testing.T) {
		newPodWaitLister = func() (waitfor.PodLister, error) { return nil, api.ErrNoCredentials }
		cmd, _ := waitCommand()
		_, err := waitForPodSSH(context.Background(), cmd, "pod-1", time.Minute)
		if !errors.Is(err, api.ErrNoCredentials) {
			t.Fatalf("expected the no_credentials sentinel, got %v", err)
		}
	})
}

// resourceIDOf reads the machine-readable resource id off an error, the way
// internal/output does when it emits the json error object.
func resourceIDOf(err error) string {
	var ider interface{ ErrorResourceID() string }
	if errors.As(err, &ider) {
		return ider.ErrorResourceID()
	}
	return ""
}

// A wait that is cancelled (ctrl-c) must not lose the pod: same coded error,
// same id, different code from a timeout.
func TestWaitForPodSSHInterrupted(t *testing.T) {
	oldLister, oldProbe, oldInterval := newPodWaitLister, podSSHProbe, waitPollInterval
	t.Cleanup(func() { newPodWaitLister, podSSHProbe, waitPollInterval = oldLister, oldProbe, oldInterval })
	waitPollInterval = time.Millisecond

	lister := &fakePodLister{pods: []*api.LegacyPod{runningPodWithSSH("pod-1", false)}}
	newPodWaitLister = func() (waitfor.PodLister, error) { return lister, nil }

	cancelled, cancel := context.WithCancel(context.Background())
	cancel() // stand in for the signal arriving during the wait

	cmd, _ := waitCommand()
	_, err := waitForPodSSH(cancelled, cmd, "pod-1", time.Minute)
	var waitErr *waitfor.Error
	if !errors.As(err, &waitErr) {
		t.Fatalf("expected a *waitfor.Error, got %#v", err)
	}
	if waitErr.ErrorCode() != waitfor.CodeInterrupted {
		t.Errorf("code = %q, want %q", waitErr.ErrorCode(), waitfor.CodeInterrupted)
	}
	if resourceIDOf(err) != "pod-1" {
		t.Errorf("a cancelled wait must still carry the pod id, got %q", resourceIDOf(err))
	}
	if !strings.Contains(err.Error(), "runpodctl pod delete pod-1") {
		t.Errorf("error = %q, want the delete command", err.Error())
	}
}

// The signal handler must cover the whole post-create phase, not just the poll
// loop: a ctrl-c after readiness but during the re-read used to take the default
// disposition (exit 130, empty stdout) and lose the id of a pod that bills.
//
// This drives waitForReadyPod, not the two halves separately, because the bug was
// exactly in how they were wired together: the interrupt is delivered at the
// moment ssh answers, i.e. in the gap the old code left uncovered.
func TestWaitForReadyPodCoversTheReReadWithTheSameSignalContext(t *testing.T) {
	oldLister, oldProbe, oldInterval := newPodWaitLister, podSSHProbe, waitPollInterval
	oldNotify := notifyWaitSignals
	oldFetch, oldTries, oldBackoff := fetchPodDetailsFn, postWaitReadTries, postWaitReadBackoff
	t.Cleanup(func() {
		newPodWaitLister, podSSHProbe, waitPollInterval = oldLister, oldProbe, oldInterval
		notifyWaitSignals = oldNotify
		fetchPodDetailsFn, postWaitReadTries, postWaitReadBackoff = oldFetch, oldTries, oldBackoff
	})
	waitPollInterval = time.Millisecond
	// short, not long: a build that ignores the cancellation must fail on the
	// assertions below rather than hang until the go test timeout.
	postWaitReadBackoff = time.Millisecond
	postWaitReadTries = 3

	newPodWaitLister = func() (waitfor.PodLister, error) {
		return &fakePodLister{pods: []*api.LegacyPod{runningPodWithSSH("pod-1", true)}}, nil
	}

	var registered []os.Signal
	var interrupt context.CancelFunc
	// released reports that the signal registration was torn down. waitfor
	// .SignalContext does that as soon as the first signal lands; a plain
	// notifyWaitSignals would only do it on the deferred stop, i.e. after the
	// re-read below had already finished running uninterruptibly.
	released := make(chan struct{})
	notifyWaitSignals = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		registered = signals
		ctx, cancel := context.WithCancel(parent)
		interrupt = cancel
		return ctx, func() {
			cancel()
			select {
			case <-released:
			default:
				close(released)
			}
		}
	}
	// the ctrl-c lands exactly as ssh comes up, so the wait itself succeeds and
	// the cancellation can only be observed by the re-read.
	podSSHProbe = func(context.Context, string) error {
		interrupt()
		return nil
	}

	calls := 0
	releasedDuringReRead := false
	degraded := &podDetails{Pod: &api.Pod{ID: "pod-1"}, SSH: map[string]interface{}{"error": "ssh info unavailable"}}
	fetchPodDetailsFn = func(string, bool, bool) (*podDetails, error) {
		calls++
		// the re-read stands in for "work still in flight when the signal lands":
		// its api calls take a client timeout to give up, so the handler has to be
		// released by now or a second ctrl-c would be swallowed for that long.
		select {
		case <-released:
			releasedDuringReRead = true
		case <-time.After(2 * time.Second):
		}
		return degraded, nil
	}

	cmd, _ := waitCommand()
	_, err := waitForReadyPod(cmd, "pod-1", time.Minute)
	if err == nil {
		t.Fatal("expected an interrupted error, not a payload")
	}
	if calls != 1 {
		t.Errorf("read the pod %d times, want 1: the re-read must see the same cancellation", calls)
	}
	if !releasedDuringReRead {
		t.Error("the signal registration must be released before the re-read runs, or a second ctrl-c is swallowed while it does")
	}
	var waitErr *waitfor.Error
	if !errors.As(err, &waitErr) || waitErr.ErrorCode() != waitfor.CodeInterrupted {
		t.Errorf("error must be a %s-coded *waitfor.Error, got %#v", waitfor.CodeInterrupted, err)
	}
	if resourceIDOf(err) != "pod-1" {
		t.Errorf("error id = %q, want pod-1", resourceIDOf(err))
	}

	wantSignals := map[os.Signal]bool{os.Interrupt: false, syscall.SIGTERM: false}
	for _, sig := range registered {
		wantSignals[sig] = true
	}
	for sig, seen := range wantSignals {
		if !seen {
			t.Errorf("the wait must register for %v", sig)
		}
	}
}

// The window this closes: ssh is already up, so the pod is the good outcome — but
// the re-read sleeps between attempts, and an interrupt there must still exit with
// a coded error object naming the pod rather than the shell's bare 130.
func TestPodDetailsWithSSHInterrupted(t *testing.T) {
	oldFetch, oldTries, oldBackoff := fetchPodDetailsFn, postWaitReadTries, postWaitReadBackoff
	t.Cleanup(func() {
		fetchPodDetailsFn, postWaitReadTries, postWaitReadBackoff = oldFetch, oldTries, oldBackoff
	})
	// short: an implementation that ignored the cancellation must fail on the call
	// count below, not hang until the go test timeout.
	postWaitReadBackoff = time.Millisecond
	postWaitReadTries = 3

	degraded := &podDetails{Pod: &api.Pod{ID: "pod-1"}, SSH: map[string]interface{}{"error": "ssh info unavailable"}}
	calls := 0
	fetchPodDetailsFn = func(string, bool, bool) (*podDetails, error) {
		calls++
		return degraded, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := podDetailsWithSSH(ctx, "pod-1", "1.2.3.4:51227")
	if err == nil {
		t.Fatal("expected an error rather than a silent 130")
	}
	if calls != 1 {
		t.Errorf("read the pod %d times, want 1: the cancellation must stop the retries", calls)
	}
	// the same type Until returns for an interrupted poll loop, so a consumer can
	// type-test "the wait was interrupted" once rather than per phase.
	var waitErr *waitfor.Error
	if !errors.As(err, &waitErr) || waitErr.ErrorCode() != waitfor.CodeInterrupted {
		t.Errorf("error must be a %s-coded *waitfor.Error, got %#v", waitfor.CodeInterrupted, err)
	}
	if resourceIDOf(err) != "pod-1" {
		t.Errorf("error id = %q, want pod-1: an interrupt here must not lose a billed pod", resourceIDOf(err))
	}
	for _, want := range []string{"pod-1", "1.2.3.4:51227", "runpodctl pod delete pod-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// A pod that has exited can never become reachable, so the wait must end at once
// rather than bill out the full timeout.
func TestWaitForPodSSHFailsFastOnATerminalPod(t *testing.T) {
	oldLister, oldProbe, oldInterval := newPodWaitLister, podSSHProbe, waitPollInterval
	t.Cleanup(func() { newPodWaitLister, podSSHProbe, waitPollInterval = oldLister, oldProbe, oldInterval })
	waitPollInterval = time.Hour // any poll after the first would sleep out the budget

	lister := &fakePodLister{pods: []*api.LegacyPod{{ID: "pod-1", DesiredStatus: "EXITED"}}}
	newPodWaitLister = func() (waitfor.PodLister, error) { return lister, nil }

	cmd, _ := waitCommand()
	// a tiny timeout as well as a huge interval: the fail-fast still proves itself
	// with calls == 1, but a regression fails in 300ms with a wait_timeout instead
	// of hanging until `go test` panics after 10 minutes.
	_, err := waitForPodSSH(context.Background(), cmd, "pod-1", 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected an error for an exited pod")
	}
	if lister.calls != 1 {
		t.Errorf("polled %d times, want 1: a terminal status must stop the wait", lister.calls)
	}
	var fatal *waitfor.FatalError
	if !errors.As(err, &fatal) || fatal.ErrorCode() != "conflict" {
		t.Fatalf("expected a conflict-coded fatal error, got %#v", err)
	}
	if resourceIDOf(err) != "pod-1" {
		t.Errorf("error id = %q, want pod-1", resourceIDOf(err))
	}
}

// --wait prints the `pod get` shape rather than the create response, because the
// ssh block is the entire product of waiting. A re-read that silently degrades
// to {"ssh":{"error":...}} must not be reported as success.
func TestPodDetailsWithSSH(t *testing.T) {
	oldFetch, oldTries, oldBackoff := fetchPodDetailsFn, postWaitReadTries, postWaitReadBackoff
	t.Cleanup(func() {
		fetchPodDetailsFn, postWaitReadTries, postWaitReadBackoff = oldFetch, oldTries, oldBackoff
	})
	postWaitReadBackoff = 0

	ready := &podDetails{
		Pod: &api.Pod{ID: "pod-1"},
		SSH: map[string]interface{}{"ssh_command": "ssh -i k root@1.2.3.4 -p 51227", "port": 51227},
	}
	degraded := &podDetails{Pod: &api.Pod{ID: "pod-1"}, SSH: map[string]interface{}{"error": "ssh info unavailable"}}

	t.Run("returns the enriched read", func(t *testing.T) {
		calls := 0
		fetchPodDetailsFn = func(string, bool, bool) (*podDetails, error) {
			calls++
			return ready, nil
		}
		got, err := podDetailsWithSSH(context.Background(), "pod-1", "1.2.3.4:51227")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.SSH["ssh_command"] == nil {
			t.Errorf("expected the live ssh command in the payload: %v", got.SSH)
		}
		if calls != 1 {
			t.Errorf("read the pod %d times, want 1", calls)
		}
	})

	t.Run("retries a degraded ssh block", func(t *testing.T) {
		calls := 0
		fetchPodDetailsFn = func(string, bool, bool) (*podDetails, error) {
			calls++
			if calls == 1 {
				return degraded, nil
			}
			return ready, nil
		}
		got, err := podDetailsWithSSH(context.Background(), "pod-1", "1.2.3.4:51227")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.SSH["ssh_command"] == nil {
			t.Errorf("expected the live ssh command after the retry: %v", got.SSH)
		}
		if calls != 2 {
			t.Errorf("read the pod %d times, want 2", calls)
		}
	})

	t.Run("a permanently degraded ssh block is an error, not a silent success", func(t *testing.T) {
		fetchPodDetailsFn = func(string, bool, bool) (*podDetails, error) { return degraded, nil }
		_, err := podDetailsWithSSH(context.Background(), "pod-1", "1.2.3.4:51227")
		if err == nil {
			t.Fatal("expected an error rather than a payload with no ssh info")
		}
		for _, want := range []string{"pod-1", "1.2.3.4:51227", "ssh info unavailable", "runpodctl pod get pod-1"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), want)
			}
		}
		if resourceIDOf(err) != "pod-1" {
			t.Errorf("error id = %q, want pod-1", resourceIDOf(err))
		}
	})

	t.Run("a failing read names the pod", func(t *testing.T) {
		fetchPodDetailsFn = func(string, bool, bool) (*podDetails, error) {
			return nil, errors.New("failed to get pod: boom")
		}
		_, err := podDetailsWithSSH(context.Background(), "pod-1", "1.2.3.4:51227")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "pod-1") || !strings.Contains(err.Error(), "boom") {
			t.Errorf("error = %q, want the pod id and the cause", err.Error())
		}
		if resourceIDOf(err) != "pod-1" {
			t.Errorf("error id = %q, want pod-1", resourceIDOf(err))
		}
	})
}

// The CON-842 regression was the wire format: the rest POST /pods schema has
// no dockerArgs field and 400s on it, so the request must serialize the start
// command as dockerStartCmd and never as dockerArgs.
func TestPodCreateRequestDockerArgsWireFormat(t *testing.T) {
	cmd, entrypoint := parseDockerArgs("sleep infinity")
	body, err := json.Marshal(&api.PodCreateRequest{
		ImageName:        "ubuntu:22.04",
		ComputeType:      "CPU",
		DockerStartCmd:   cmd,
		DockerEntrypoint: entrypoint,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(body), "dockerArgs") {
		t.Fatalf("request body must not contain dockerArgs: %s", body)
	}
	if !strings.Contains(string(body), `"dockerStartCmd":["sleep","infinity"]`) {
		t.Fatalf("request body missing dockerStartCmd tokens: %s", body)
	}
}

func TestResolvePodSchedule(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name           string
		computeType    string
		stopAfter      string
		terminateAfter string
		wantStop       string
		wantTerminate  string
		wantErr        string
	}{
		{name: "unset", computeType: "GPU"},
		{
			name:        "duration is resolved against now",
			computeType: "GPU",
			stopAfter:   "2h",
			wantStop:    "2026-04-15T14:00:00Z",
		},
		{
			name:           "days are supported",
			computeType:    "GPU",
			terminateAfter: "7d",
			wantTerminate:  "2026-04-22T12:00:00Z",
		},
		{
			name:        "rfc3339 is normalised to utc",
			computeType: "GPU",
			stopAfter:   "2026-04-15T16:30:00+02:00",
			wantStop:    "2026-04-15T14:30:00Z",
		},
		{
			name:        "past timestamp is rejected",
			computeType: "GPU",
			stopAfter:   "2020-01-01T00:00:00Z",
			wantErr:     "must be in the future",
		},
		{
			name:        "garbage is rejected",
			computeType: "GPU",
			stopAfter:   "tomorrow",
			wantErr:     "invalid --stop-after",
		},
		{
			name:        "cpu pods are refused rather than silently dropped",
			computeType: "CPU",
			stopAfter:   "2h",
			wantErr:     "not supported for compute type CPU",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer withScheduleFlags(t, tc.stopAfter, tc.terminateAfter, now)()

			cmd := &cobra.Command{}
			cmd.SetErr(&bytes.Buffer{})

			got, err := resolvePodSchedule(cmd, tc.computeType)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.stopAfter != tc.wantStop || got.terminateAfter != tc.wantTerminate {
				t.Fatalf("got %+v, want stop=%q terminate=%q", got, tc.wantStop, tc.wantTerminate)
			}
		})
	}
}

func TestResolvePodScheduleNotesTheDeadline(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	defer withScheduleFlags(t, "2h", "", now)()

	var stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&stderr)

	if _, err := resolvePodSchedule(cmd, "GPU"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "auto-stop scheduled for 2026-04-15T14:00:00Z") {
		t.Fatalf("stderr = %q, want the resolved stop time", stderr.String())
	}
}

// withScheduleFlags sets the create flags and clock resolvePodSchedule reads,
// and returns the restore func.
func withScheduleFlags(t *testing.T, stopAfter, terminateAfter string, now time.Time) func() {
	t.Helper()
	prevStop, prevTerminate, prevNow := createStopAfter, createTerminateAfter, timeNow
	createStopAfter, createTerminateAfter = stopAfter, terminateAfter
	timeNow = func() time.Time { return now }
	return func() {
		createStopAfter, createTerminateAfter, timeNow = prevStop, prevTerminate, prevNow
	}
}
