package sshconnect

import (
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/podstate"
)

func port(private, public int, isPublic bool) *api.LegacyPort {
	return &api.LegacyPort{Ip: "1.2.3.4", IsIpPublic: isPublic, PrivatePort: private, PublicPort: public}
}

func TestNotReadyMessage(t *testing.T) {
	tests := []struct {
		name         string
		state        podstate.State
		declared     []string
		runtimePorts []*api.LegacyPort
		want         string
	}{
		{
			name:  "initializing explains the pod, not the port",
			state: podstate.State{Status: podstate.StatusInitializing, Reason: podstate.ReasonAwaitingContainer},
			want:  "pod not ready: no container reported yet (image pull, container create or boot)",
		},
		{
			name:  "stopped points at pod start",
			state: podstate.State{Status: podstate.StatusStopped, Reason: podstate.ReasonStoppedByUser},
			want:  "pod not ready: pod is stopped; start it with 'runpodctl pod start <pod-id>'",
		},
		{
			name:  "terminated says so",
			state: podstate.State{Status: podstate.StatusTerminated, Reason: podstate.ReasonTerminatedOutbid},
			want:  "pod not ready: pod is terminated",
		},
		{
			// unknown adds nothing, so the message must not gain a dangling colon.
			name:  "unknown keeps the bare message",
			state: podstate.State{Status: podstate.StatusUnknown, Reason: podstate.ReasonRuntimeUnavailable},
			want:  "pod not ready",
		},

		// --- running: the container is up, so the port is what is missing. the
		// remedy differs per case, and telling someone to destroy a paid pod --
		// or to add a flag it already carries -- is the failure mode to avoid.
		{
			name:     "running without 22 declared points at pod update",
			state:    podstate.State{Status: podstate.StatusRunning},
			declared: []string{"8888/http"},
			want:     "pod not ready: pod does not publish 22/tcp; add it with 'runpodctl pod update <pod-id> --ports 22/tcp'",
		},
		{
			name:         "running with 22 declared but not publicly routable offers no bogus remedy",
			state:        podstate.State{Status: podstate.StatusRunning},
			declared:     []string{"22/tcp", "8888/http"},
			runtimePorts: []*api.LegacyPort{port(22, 40022, false), port(8888, 40088, false)},
			want:         "pod not ready: port 22 is mapped but not publicly routable on this machine",
		},
		{
			name:         "running with 22 declared and no mapping yet says the mapping is pending",
			state:        podstate.State{Status: podstate.StatusRunning},
			declared:     []string{"22/tcp"},
			runtimePorts: []*api.LegacyPort{port(8888, 40088, true)},
			want:         "pod not ready: port 22 is declared but the host has not published a mapping for it yet",
		},
		{
			name:     "a bare port number counts as declared",
			state:    podstate.State{Status: podstate.StatusRunning},
			declared: []string{" 22 "},
			want:     "pod not ready: port 22 is declared but the host has not published a mapping for it yet",
		},
		{
			name:  "running with no declared ports at all points at pod update",
			state: podstate.State{Status: podstate.StatusRunning},
			want:  "pod not ready: pod does not publish 22/tcp; add it with 'runpodctl pod update <pod-id> --ports 22/tcp'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NotReadyMessage(tt.state, tt.declared, tt.runtimePorts)
			if got != tt.want {
				t.Errorf("NotReadyMessage() = %q, want %q", got, tt.want)
			}
			if got != strings.ToLower(got) {
				t.Errorf("message must be lowercase: %q", got)
			}
		})
	}
}

// TestNotReadyMessageNeverSuggestsARedundantFlag is the regression guard for the
// advice bug: whenever 22 is already declared, the message must not tell the
// caller to add --ports 22/tcp.
func TestNotReadyMessageNeverSuggestsARedundantFlag(t *testing.T) {
	for _, runtimePorts := range [][]*api.LegacyPort{
		nil,
		{port(22, 40022, false)},
		{port(8888, 40088, true)},
	} {
		msg := NotReadyMessage(podstate.State{Status: podstate.StatusRunning}, []string{"22/tcp"}, runtimePorts)
		if strings.Contains(msg, "--ports 22/tcp") {
			t.Errorf("told the caller to add a port the pod already declares: %q", msg)
		}
		if strings.Contains(msg, "recreate") {
			t.Errorf("suggested recreating a running pod: %q", msg)
		}
	}
}

func TestSplitPorts(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"22/tcp", []string{"22/tcp"}},
		{"22/tcp,8888/http", []string{"22/tcp", "8888/http"}},
		{" 22/tcp , 8888/http ,", []string{"22/tcp", "8888/http"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := SplitPorts(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitPorts(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("SplitPorts(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestPodState(t *testing.T) {
	tests := []struct {
		name       string
		pod        *api.LegacyPod
		wantStatus podstate.Status
	}{
		{
			name:       "nil pod is unknown",
			pod:        nil,
			wantStatus: podstate.StatusUnknown,
		},
		{
			name:       "running with telemetry",
			pod:        &api.LegacyPod{DesiredStatus: "RUNNING", Runtime: &api.LegacyRuntime{}},
			wantStatus: podstate.StatusRunning,
		},
		{
			name:       "running without telemetry",
			pod:        &api.LegacyPod{DesiredStatus: "RUNNING"},
			wantStatus: podstate.StatusInitializing,
		},
		{
			name:       "exited with stale telemetry is still stopped",
			pod:        &api.LegacyPod{DesiredStatus: "EXITED", Runtime: &api.LegacyRuntime{}},
			wantStatus: podstate.StatusStopped,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodState(tt.pod); got.Status != tt.wantStatus {
				t.Errorf("PodState().Status = %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}

// TestListConnectionsSkipsDeadPods is the regression guard for `ssh connect`
// with no args, which handed back a working-looking ssh command for a stopped
// pod because a stopped pod keeps reporting its old runtime ports.
func TestListConnectionsSkipsDeadPods(t *testing.T) {
	stale := &api.LegacyRuntime{Ports: []*api.LegacyPort{port(22, 40022, true)}}
	pods := []*api.LegacyPod{
		{ID: "up", Name: "up", DesiredStatus: "RUNNING", Runtime: stale},
		{ID: "stopped", Name: "stopped", DesiredStatus: "EXITED", LastStatusChange: "Exited by user: x", Runtime: stale},
		{ID: "gone", Name: "gone", DesiredStatus: "TERMINATED", Runtime: stale},
	}

	conns := ListConnections(pods, KeyInfo{})
	if len(conns) != 1 {
		t.Fatalf("expected only the running pod, got %d: %v", len(conns), conns)
	}
	if conns[0]["id"] != "up" {
		t.Errorf("listed the wrong pod: %v", conns[0])
	}
}
