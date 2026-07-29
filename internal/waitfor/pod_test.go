package waitfor

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

type fakePodLister struct {
	pods []*api.LegacyPod
	err  error
}

func (f *fakePodLister) GetPods() ([]*api.LegacyPod, error) { return f.pods, f.err }

func podWithSSHPort(id string, status string, privatePort, publicPort int, public bool) *api.LegacyPod {
	return &api.LegacyPod{
		ID:            id,
		DesiredStatus: status,
		Runtime: &api.LegacyRuntime{
			Ports: []*api.LegacyPort{{
				Ip:          "1.2.3.4",
				IsIpPublic:  public,
				PrivatePort: privatePort,
				PublicPort:  publicPort,
				PortType:    "tcp",
			}},
		},
	}
}

func TestPodSSHPoller(t *testing.T) {
	probeErr := errors.New("dial tcp 1.2.3.4:51227: connect: connection refused")

	cases := []struct {
		name       string
		pods       []*api.LegacyPod
		probe      Prober
		wantReady  bool
		wantDetail string
		wantProbed string
	}{
		{
			name:       "pod not in the list yet",
			pods:       nil,
			wantDetail: "pod not listed yet",
		},
		{
			name:       "pod not running yet",
			pods:       []*api.LegacyPod{podWithSSHPort("pod-1", "EXITED", 22, 51227, true)},
			wantDetail: "pod status exited",
		},
		{
			name:       "no ssh port allocated",
			pods:       []*api.LegacyPod{podWithSSHPort("pod-1", "RUNNING", 8888, 20000, true)},
			wantDetail: "ssh port not allocated yet",
		},
		{
			// the port has to be publicly mapped; a private-only mapping is not
			// reachable from the machine running the cli.
			name:       "ssh port is not public",
			pods:       []*api.LegacyPod{podWithSSHPort("pod-1", "RUNNING", 22, 51227, false)},
			wantDetail: "ssh port not allocated yet",
		},
		{
			// the regression this whole probe exists for: prod allocates port 22
			// for an image with no sshd, so "listed" must not mean "ready".
			name:       "allocated port that refuses the connection is not ready",
			pods:       []*api.LegacyPod{podWithSSHPort("pod-1", "RUNNING", 22, 51227, true)},
			probe:      func(context.Context, string) error { return probeErr },
			wantDetail: "ssh port 1.2.3.4:51227 allocated but not reachable: dial tcp",
			wantProbed: "1.2.3.4:51227",
		},
		{
			name:       "reachable ssh is ready",
			pods:       []*api.LegacyPod{podWithSSHPort("pod-1", "RUNNING", 22, 51227, true)},
			probe:      func(context.Context, string) error { return nil },
			wantReady:  true,
			wantDetail: "ssh reachable at 1.2.3.4:51227",
			wantProbed: "1.2.3.4:51227",
		},
		{
			name: "matches on pod id, ignoring other pods",
			pods: []*api.LegacyPod{
				podWithSSHPort("other", "RUNNING", 22, 40000, true),
				podWithSSHPort("pod-1", "RUNNING", 22, 51227, true),
			},
			probe:      func(context.Context, string) error { return nil },
			wantReady:  true,
			wantProbed: "1.2.3.4:51227",
		},
		{
			name:       "a nil runtime is not ready",
			pods:       []*api.LegacyPod{{ID: "pod-1", DesiredStatus: "RUNNING"}},
			wantDetail: "ssh port not allocated yet",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probed := ""
			probe := tc.probe
			if probe != nil {
				inner := probe
				probe = func(ctx context.Context, addr string) error {
					probed = addr
					return inner(ctx, addr)
				}
			}

			state, err := PodSSHPoller(&fakePodLister{pods: tc.pods}, "pod-1", probe)(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state.Ready != tc.wantReady {
				t.Errorf("ready = %v, want %v", state.Ready, tc.wantReady)
			}
			if tc.wantDetail != "" && !strings.Contains(state.Detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", state.Detail, tc.wantDetail)
			}
			if probed != tc.wantProbed {
				t.Errorf("probed %q, want %q", probed, tc.wantProbed)
			}
		})
	}
}

// A failing pod lookup must abort the wait rather than burn the whole budget on
// an error that will not fix itself (e.g. a revoked api key).
func TestPodSSHPollerReturnsListErrors(t *testing.T) {
	_, err := PodSSHPoller(&fakePodLister{err: errors.New("unauthorized")}, "pod-1", nil)(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected the lister error to propagate, got %v", err)
	}
}

// ProbeSSH must accept only something that actually speaks ssh, so that a port
// which merely accepts tcp does not read as "ssh is up".
func TestProbeSSH(t *testing.T) {
	cases := []struct {
		name    string
		banner  string
		wantErr string
	}{
		{name: "ssh banner", banner: "SSH-2.0-OpenSSH_9.6\r\n"},
		{name: "http server is not ssh", banner: "HTTP/1.1 400 Bad Request\r\n", wantErr: "is not an ssh server"},
		{name: "silent listener", banner: "", wantErr: "no ssh banner"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer listener.Close() //nolint:errcheck

			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer conn.Close() //nolint:errcheck
				if tc.banner != "" {
					_, _ = conn.Write([]byte(tc.banner))
				}
			}()

			err = ProbeSSH(context.Background(), listener.Addr().String())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestProbeSSHOnClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := ProbeSSH(context.Background(), addr); err == nil {
		t.Fatal("expected a dial error for a closed port")
	}
}
