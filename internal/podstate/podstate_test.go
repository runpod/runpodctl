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
				DesiredStatus:    "RUNNING",
				LastStatusChange: "Rented by User: Wed Jul 29 2026 21:51:59 GMT+0000",
				RuntimeProbed:    true,
				RuntimeReported:  true,
			},
			wantStatus: StatusRunning,
		},
		{
			name: "running without telemetry is initializing",
			signals: Signals{
				DesiredStatus:    "RUNNING",
				LastStatusChange: "Rented by User: Wed Jul 29 2026 21:51:59 GMT+0000",
				RuntimeProbed:    true,
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

		// --- EXITED: telemetry is deliberately ignored. a stopped pod keeps
		// reporting stale runtime for a while, so consulting it would report a
		// stopped pod as running.
		{
			name: "exited by user",
			signals: Signals{
				DesiredStatus:    "EXITED",
				LastStatusChange: "Exited by user: Wed Jul 29 2026 21:56:48 GMT+0000",
				RuntimeProbed:    true,
				RuntimeReported:  true,
			},
			wantStatus: StatusStopped,
			wantReason: ReasonStoppedByUser,
		},
		{
			name: "exited by runpod",
			signals: Signals{
				DesiredStatus:    "EXITED",
				LastStatusChange: "Exited by Runpod: Wed Jul 29 2026 21:56:48 GMT+0000",
				RuntimeProbed:    true,
			},
			wantStatus: StatusStopped,
			wantReason: ReasonStoppedByRunpod,
		},
		{
			// the backend spells it both "Runpod" and "RunPod"; a
			// case-sensitive check silently stops matching.
			name: "exited by runpod with legacy capitalisation",
			signals: Signals{
				DesiredStatus:    "EXITED",
				LastStatusChange: "Exited by RunPod: Insufficient balance",
			},
			wantStatus: StatusStopped,
			wantReason: ReasonStoppedByRunpod,
		},
		{
			name:       "exited with unattributed status change has no reason",
			signals:    Signals{DesiredStatus: "EXITED", LastStatusChange: "something else"},
			wantStatus: StatusStopped,
		},
		{
			name:       "exited with nil status change has no reason",
			signals:    Signals{DesiredStatus: "EXITED"},
			wantStatus: StatusStopped,
		},
		{
			name:       "exited with non-string status change has no reason",
			signals:    Signals{DesiredStatus: "EXITED", LastStatusChange: 12345},
			wantStatus: StatusStopped,
		},

		// --- TERMINATED ---
		{
			name: "terminated by user",
			signals: Signals{
				DesiredStatus:    "TERMINATED",
				LastStatusChange: "Terminated by user: Wed Jul 29 2026 21:58:00 GMT+0000",
			},
			wantStatus: StatusTerminated,
			wantReason: ReasonTerminatedByUser,
		},
		{
			name: "terminated by runpod",
			signals: Signals{
				DesiredStatus:    "TERMINATED",
				LastStatusChange: "Terminated by RunPod: Wed Jul 29 2026 21:58:00 GMT+0000",
			},
			wantStatus: StatusTerminated,
			wantReason: ReasonTerminatedByRunpod,
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
		string(ReasonAwaitingContainer), string(ReasonStoppedByUser),
		string(ReasonStoppedByRunpod), string(ReasonTerminatedByUser),
		string(ReasonTerminatedByRunpod), string(ReasonRuntimeUnavailable),
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

func TestSSHUnavailableReason(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{
			name:  "running blames the missing port, not the boot",
			state: State{Status: StatusRunning},
			want:  "no public port 22 mapped; recreate the pod with --ports 22/tcp",
		},
		{
			name:  "initializing blames the container start",
			state: State{Status: StatusInitializing, Reason: ReasonAwaitingContainer},
			want:  "container is still starting (image pull, container create, or boot)",
		},
		{
			name:  "stopped points at pod start",
			state: State{Status: StatusStopped, Reason: ReasonStoppedByUser},
			want:  "pod is stopped; start it with 'runpodctl pod start <pod-id>'",
		},
		{
			name:  "terminated",
			state: State{Status: StatusTerminated},
			want:  "pod is terminated",
		},
		{
			name:  "unknown adds nothing",
			state: State{Status: StatusUnknown, Reason: ReasonRuntimeUnavailable},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SSHUnavailableReason(tt.state); got != tt.want {
				t.Errorf("SSHUnavailableReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
