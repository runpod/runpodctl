package waitfor

import (
	"context"
	"errors"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

// TestPodRunningPoller covers the status-based readiness used by
// `pod create --wait-for running`: it never probes ssh, so a cpu pod with no
// sshd (which the ssh poller can only ever time out on) becomes waitable.
// Readiness is desiredStatus RUNNING *and* runtime reported (container up),
// reusing the same running/initializing distinction as `pod get`.
func TestPodRunningPoller(t *testing.T) {
	cases := []struct {
		name       string
		pods       []*api.LegacyPod
		wantReady  bool
		wantFatal  bool
		wantCode   string
		wantDetail string
	}{
		{
			name:       "running with runtime reported -> ready",
			pods:       []*api.LegacyPod{podWithSSHPort("pod-1", "RUNNING", 8888, 20000, false)},
			wantReady:  true,
			wantDetail: "pod pod-1 is running",
		},
		{
			name:       "running but no runtime yet -> keep waiting",
			pods:       []*api.LegacyPod{{ID: "pod-1", DesiredStatus: "RUNNING"}},
			wantDetail: "pod running, container not up yet",
		},
		{
			name:       "not running yet -> keep waiting",
			pods:       []*api.LegacyPod{{ID: "pod-1", DesiredStatus: "CREATED"}},
			wantDetail: "pod status created",
		},
		{
			name:      "terminal status -> fatal conflict",
			pods:      []*api.LegacyPod{{ID: "pod-1", DesiredStatus: "EXITED"}},
			wantFatal: true,
			wantCode:  "conflict",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			poll := PodRunningPoller(&fakePodLister{pods: tc.pods}, "pod-1")
			state, err := poll(context.Background())

			if tc.wantFatal {
				var fe *FatalError
				if !errors.As(err, &fe) {
					t.Fatalf("expected a FatalError, got %v", err)
				}
				if fe.Code != tc.wantCode {
					t.Errorf("code = %q, want %q", fe.Code, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.Ready != tc.wantReady {
				t.Errorf("ready = %v, want %v", state.Ready, tc.wantReady)
			}
			if tc.wantDetail != "" && state.Detail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", state.Detail, tc.wantDetail)
			}
		})
	}
}
