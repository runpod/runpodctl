package podstate

import "testing"

func TestDerive(t *testing.T) {
	tests := []struct {
		name       string
		signals    Signals
		wantStatus Status
		wantReason Reason
	}{
		// --- RUNNING: the only branch that consults runtime telemetry ---
		{
			name: "running with telemetry is running",
			signals: Signals{
				DesiredStatus:   "RUNNING",
				RuntimeProbed:   true,
				RuntimeReported: true,
			},
			wantStatus: StatusRunning,
		},
		{
			name: "running without telemetry is initializing",
			signals: Signals{
				DesiredStatus: "RUNNING",
				RuntimeProbed: true,
			},
			wantStatus: StatusInitializing,
			wantReason: ReasonAwaitingContainer,
		},
		{
			name:       "running with no probe is unknown, not initializing",
			signals:    Signals{DesiredStatus: "RUNNING"},
			wantStatus: StatusUnknown,
			wantReason: ReasonRuntimeUnavailable,
		},
		{
			name:       "desired status is matched case-insensitively",
			signals:    Signals{DesiredStatus: " running ", RuntimeProbed: true, RuntimeReported: true},
			wantStatus: StatusRunning,
		},

		// --- EXITED/TERMINATED: telemetry is deliberately ignored. a stopped
		// pod keeps reporting stale runtime for a while, so consulting it would
		// report a stopped pod as running.
		{
			name:       "exited is stopped, with no reason",
			signals:    Signals{DesiredStatus: "EXITED", RuntimeProbed: true, RuntimeReported: true},
			wantStatus: StatusStopped,
		},
		{
			name:       "terminated is terminated, with no reason",
			signals:    Signals{DesiredStatus: "TERMINATED", RuntimeProbed: true},
			wantStatus: StatusTerminated,
		},

		// --- statuses the platform defines but does not surface in practice.
		// they must not be guessed into running/stopped.
		{
			name:       "created is unknown",
			signals:    Signals{DesiredStatus: "CREATED", RuntimeProbed: true},
			wantStatus: StatusUnknown,
		},
		{
			name:       "restarting is unknown",
			signals:    Signals{DesiredStatus: "RESTARTING", RuntimeProbed: true, RuntimeReported: true},
			wantStatus: StatusUnknown,
		},
		{
			name:       "paused is unknown",
			signals:    Signals{DesiredStatus: "PAUSED"},
			wantStatus: StatusUnknown,
		},
		{
			name:       "v2 error is unknown",
			signals:    Signals{DesiredStatus: "ERROR", RuntimeProbed: true},
			wantStatus: StatusUnknown,
		},
		{
			name:       "dead is unknown",
			signals:    Signals{DesiredStatus: "DEAD"},
			wantStatus: StatusUnknown,
		},
		{
			name:       "empty desired status is unknown",
			signals:    Signals{},
			wantStatus: StatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Derive(tt.signals)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

// TestDeriveVocabularyIsLowercase guards the repo-wide rule that output text is
// lowercase, and that no value picks up a space or capital by accident.
func TestDeriveVocabularyIsLowercase(t *testing.T) {
	values := []string{
		string(StatusRunning), string(StatusInitializing), string(StatusStopped),
		string(StatusTerminated), string(StatusUnknown),
		string(ReasonAwaitingContainer), string(ReasonRuntimeUnavailable),
	}
	for _, v := range values {
		for _, r := range v {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("%q contains an uppercase letter", v)
				break
			}
			if r == ' ' || r == '-' {
				t.Errorf("%q contains %q; use underscores", v, string(r))
				break
			}
		}
	}
}

func TestExplain(t *testing.T) {
	tests := []struct {
		name  string
		state State
		podID string
		want  string
	}{
		{
			// deliberately hedged: runtime==null is also what an upstream
			// telemetry timeout looks like, so this must not assert a boot.
			name:  "initializing does not promise the container is starting",
			state: State{Status: StatusInitializing, Reason: ReasonAwaitingContainer},
			want:  "no container reported yet (image pull, container create or boot)",
		},
		{
			name:  "stopped points at pod start with the real pod id",
			state: State{Status: StatusStopped},
			podID: "abc123",
			want:  "pod is stopped; start it with 'runpodctl pod start abc123'",
		},
		{
			name:  "stopped without a pod id keeps the placeholder",
			state: State{Status: StatusStopped},
			want:  "pod is stopped; start it with 'runpodctl pod start <pod-id>'",
		},
		{
			name:  "terminated",
			state: State{Status: StatusTerminated},
			want:  "pod is terminated",
		},
		{
			// a running pod's reachability is an ssh concern, not a pod state
			// concern: internal/sshconnect owns that text.
			name:  "running adds nothing",
			state: State{Status: StatusRunning},
			want:  "",
		},
		{
			name:  "unknown adds nothing",
			state: State{Status: StatusUnknown, Reason: ReasonRuntimeUnavailable},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Explain(tt.podID); got != tt.want {
				t.Errorf("Explain(%q) = %q, want %q", tt.podID, got, tt.want)
			}
		})
	}
}

// TestIsKnownDown guards the distinction the ssh paths depend on: "we know the
// container is gone" must not include "we could not tell", or a live connection
// gets thrown away for a pod in a state this cli does not model.
func TestIsKnownDown(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusStopped, true},
		{StatusTerminated, true},
		{StatusRunning, false},
		{StatusInitializing, false},
		{StatusUnknown, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := (State{Status: tt.status}).IsKnownDown(); got != tt.want {
				t.Errorf("IsKnownDown() = %v, want %v", got, tt.want)
			}
		})
	}
}
