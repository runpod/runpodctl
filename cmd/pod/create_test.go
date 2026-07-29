package pod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotWaitFlags(t)
			if tc.setup != nil {
				tc.setup()
			}
			cmd, stderr := waitCommand(tc.changedFlags...)

			got, err := resolveWaitTimeout(cmd, tc.computeType)
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
		if err := waitForPodSSH(cmd, "pod-1", time.Minute); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if lister.calls != 1 {
			t.Errorf("polled %d times, want 1", lister.calls)
		}
		if !strings.Contains(stderr.String(), "ssh on pod pod-1 ready after") {
			t.Errorf("progress must go to stderr: %q", stderr.String())
		}
	})

	t.Run("timeout keeps the pod and names it", func(t *testing.T) {
		lister := &fakePodLister{pods: []*api.LegacyPod{runningPodWithSSH("pod-1", true)}}
		newPodWaitLister = func() (waitfor.PodLister, error) { return lister, nil }
		podSSHProbe = func(context.Context, string) error {
			return errors.New("connect: connection refused")
		}

		cmd, stderr := waitCommand()
		err := waitForPodSSH(cmd, "pod-1", 2*time.Millisecond)
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
		for _, want := range []string{
			"pod pod-1 was created and is still billing",
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
		err := waitForPodSSH(cmd, "pod-1", time.Minute)
		if !errors.Is(err, api.ErrNoCredentials) {
			t.Fatalf("expected the no_credentials sentinel, got %v", err)
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
