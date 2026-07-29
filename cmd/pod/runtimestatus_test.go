package pod

import (
	"encoding/json"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/podstate"
)

func intPtr(v int) *int { return &v }

var runningState = podstate.State{Status: podstate.StatusRunning}

func TestRuntimeUptime(t *testing.T) {
	tests := []struct {
		name    string
		state   podstate.State
		runtime *api.LegacyRuntime
		want    interface{}
	}{
		{
			name:    "nil runtime yields nil so the field is omitted",
			state:   runningState,
			runtime: nil,
			want:    nil,
		},
		{
			name:    "runtime without uptime yields nil",
			state:   runningState,
			runtime: &api.LegacyRuntime{},
			want:    nil,
		},
		{
			name:    "runtime with uptime yields the value",
			state:   runningState,
			runtime: &api.LegacyRuntime{UptimeInSeconds: intPtr(261)},
			want:    261,
		},
		{
			name:    "a genuine zero uptime is reported, not swallowed",
			state:   runningState,
			runtime: &api.LegacyRuntime{UptimeInSeconds: intPtr(0)},
			want:    0,
		},
		{
			// observed live: an EXITED pod keeps reporting its last uptime.
			name:    "stale uptime on a stopped pod is dropped",
			state:   podstate.State{Status: podstate.StatusStopped, Reason: podstate.ReasonStoppedByUser},
			runtime: &api.LegacyRuntime{UptimeInSeconds: intPtr(18)},
			want:    nil,
		},
		{
			name:    "uptime is not reported while initializing",
			state:   podstate.State{Status: podstate.StatusInitializing, Reason: podstate.ReasonAwaitingContainer},
			runtime: &api.LegacyRuntime{UptimeInSeconds: intPtr(3)},
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runtimeUptime(tt.state, tt.runtime); got != tt.want {
				t.Errorf("runtimeUptime() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

// TestRuntimeUptimeOmittedFromJSON is the point of the change: `pod get` used to
// publish "uptimeSeconds": 0 forever, because rest omits the field and the
// deprecated graphql Pod.uptimeSeconds is 0 for every pod in prod.
func TestRuntimeUptimeOmittedFromJSON(t *testing.T) {
	pod := &api.Pod{ID: "abc", UptimeSeconds: runtimeUptime(runningState, nil)}
	raw, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["uptimeSeconds"]; ok {
		t.Errorf("uptimeSeconds should be absent when no container is reporting, got %s", raw)
	}

	pod.UptimeSeconds = runtimeUptime(runningState, &api.LegacyRuntime{UptimeInSeconds: intPtr(42)})
	raw, err = json.Marshal(pod)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := decoded["uptimeSeconds"]; got != float64(42) {
		t.Errorf("uptimeSeconds = %v, want 42", got)
	}
}

func TestNotReadyMessage(t *testing.T) {
	tests := []struct {
		name  string
		state podstate.State
		want  string
	}{
		{
			name:  "initializing says why",
			state: podstate.State{Status: podstate.StatusInitializing, Reason: podstate.ReasonAwaitingContainer},
			want:  "pod not ready: container is still starting (image pull, container create, or boot)",
		},
		{
			name:  "running blames the missing ssh port",
			state: podstate.State{Status: podstate.StatusRunning},
			want:  "pod not ready: no public port 22 mapped; recreate the pod with --ports 22/tcp",
		},
		{
			name:  "stopped says so",
			state: podstate.State{Status: podstate.StatusStopped, Reason: podstate.ReasonStoppedByUser},
			want:  "pod not ready: pod is stopped; start it with 'runpodctl pod start <pod-id>'",
		},
		{
			// unknown adds nothing, so the message must not gain a dangling colon.
			name:  "unknown keeps the bare message",
			state: podstate.State{Status: podstate.StatusUnknown, Reason: podstate.ReasonRuntimeUnavailable},
			want:  "pod not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notReadyMessage(tt.state); got != tt.want {
				t.Errorf("notReadyMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPodListOutputShape locks the additive output contract: the pre-existing
// keys keep their names, runtimeStatus is always present so an agent can branch
// on it unconditionally, and the two new optional keys disappear when empty.
func TestPodListOutputShape(t *testing.T) {
	tests := []struct {
		name        string
		item        podListOutput
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name: "initializing pod",
			item: podListOutput{
				ID:                  "abc",
				Name:                "p",
				DesiredStatus:       "RUNNING",
				RuntimeStatus:       string(podstate.StatusInitializing),
				RuntimeStatusReason: string(podstate.ReasonAwaitingContainer),
				ImageName:           "img",
				GpuCount:            1,
			},
			wantPresent: []string{"id", "name", "desiredStatus", "runtimeStatus", "runtimeStatusReason", "imageName", "gpuCount", "volumeInGb"},
			wantAbsent:  []string{"uptimeSeconds", "gpuId", "costPerHr", "createdAt"},
		},
		{
			name: "running pod has no reason and reports uptime",
			item: podListOutput{
				ID:            "abc",
				DesiredStatus: "RUNNING",
				RuntimeStatus: string(podstate.StatusRunning),
				UptimeSeconds: intPtr(111),
			},
			wantPresent: []string{"runtimeStatus", "uptimeSeconds"},
			wantAbsent:  []string{"runtimeStatusReason"},
		},
		{
			name:        "unknown status is still emitted, never an empty string key",
			item:        podListOutput{ID: "abc", RuntimeStatus: string(podstate.StatusUnknown)},
			wantPresent: []string{"runtimeStatus"},
			wantAbsent:  []string{"runtimeStatusReason", "uptimeSeconds"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.item)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var decoded map[string]interface{}
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for _, key := range tt.wantPresent {
				if _, ok := decoded[key]; !ok {
					t.Errorf("expected key %q in %s", key, raw)
				}
			}
			for _, key := range tt.wantAbsent {
				if _, ok := decoded[key]; ok {
					t.Errorf("expected key %q to be absent in %s", key, raw)
				}
			}
		})
	}
}
