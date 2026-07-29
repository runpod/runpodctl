package pod

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// These tests drive `pod list` and `pod get` end to end against a stub control
// plane, which is the only way the runtimeStatus wiring is actually constrained:
// the derivation itself is unit-tested in internal/podstate, but every bug worth
// catching lives in how the two commands feed it (which snapshot desiredStatus
// comes from, whether a graphql map miss counts as a probe, whether stale ports
// are gated). Those paths are unreachable from a pure function test, and the e2e
// suite is `//go:build e2e` so CI never runs it.

// stub is a fake runpod control plane: rest /pods + /pods/{id} and graphql.
type stub struct {
	restPods    []map[string]interface{}
	gqlPods     []map[string]interface{}
	gqlStatus   int // non-zero to make the graphql call fail
	graphqlHits int
}

func (s *stub) start(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/graphql") {
			s.graphqlHits++
			body, _ := io.ReadAll(r.Body)
			if s.gqlStatus != 0 {
				w.WriteHeader(s.gqlStatus)
				_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
				return
			}
			if strings.Contains(string(body), "myPods") {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{"myself": map[string]interface{}{"pods": s.gqlPods}},
				})
				return
			}
			// ssh key lookup
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"myself": map[string]interface{}{"pubKey": ""}},
			})
			return
		}

		switch {
		case r.URL.Path == "/pods":
			_ = json.NewEncoder(w).Encode(s.restPods)
		case strings.HasPrefix(r.URL.Path, "/pods/"):
			id := strings.TrimPrefix(r.URL.Path, "/pods/")
			for _, p := range s.restPods {
				if p["id"] == id {
					_ = json.NewEncoder(w).Encode(p)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", server.URL)
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL+"/graphql")
}

func restPod(id, status string, extra map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{
		"id":               id,
		"name":             id + "-name",
		"desiredStatus":    status,
		"imageName":        "img:1",
		"gpuCount":         1,
		"lastStatusChange": "Rented by User: Wed Jul 29 2026",
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func gqlPod(id, status string, runtime map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{
		"id":               id,
		"name":             id + "-name",
		"desiredStatus":    status,
		"lastStatusChange": "Rented by User: Wed Jul 29 2026",
		"runtime":          runtime,
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// capture runs fn with os.Stdout redirected and returns what it printed.
func capture(t *testing.T, fn func() error) string {
	t.Helper()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = write

	runErr := fn()

	os.Stdout = saved
	_ = write.Close()
	out, _ := io.ReadAll(read)
	_ = read.Close()

	if runErr != nil {
		t.Fatalf("command failed: %v (output: %s)", runErr, out)
	}
	return string(out)
}

func testCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func runListJSON(t *testing.T) []map[string]interface{} {
	t.Helper()
	out := capture(t, func() error { return runList(testCmd(), nil) })
	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		t.Fatalf("pod list output is not json: %v\n%s", err, out)
	}
	return items
}

func runGetJSON(t *testing.T, id string) map[string]interface{} {
	t.Helper()
	out := capture(t, func() error { return runGet(testCmd(), []string{id}) })
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("pod get output is not json: %v\n%s", err, out)
	}
	return got
}

// resetListFlags clears the package-level flag state cobra writes into, so tests
// do not leak filters into each other.
func resetListFlags(t *testing.T) {
	t.Helper()
	listComputeType, listName, listStatus, listSince, listCreatedAfter, listAll = "", "", "", "", "", true
	t.Cleanup(func() {
		listComputeType, listName, listStatus, listSince, listCreatedAfter, listAll = "", "", "", "", "", false
	})
}

func TestRunList_RuntimeStatus(t *testing.T) {
	running := map[string]interface{}{"uptimeInSeconds": 111}

	tests := []struct {
		name       string
		stub       stub
		wantStatus map[string]string
		wantReason map[string]string
		wantUptime map[string]interface{}
	}{
		{
			name: "telemetry present is running, absent is initializing",
			stub: stub{
				restPods: []map[string]interface{}{
					restPod("p-up", "RUNNING", nil),
					restPod("p-init", "RUNNING", nil),
				},
				gqlPods: []map[string]interface{}{
					gqlPod("p-up", "RUNNING", running, nil),
					gqlPod("p-init", "RUNNING", nil, nil),
				},
			},
			wantStatus: map[string]string{"p-up": "running", "p-init": "initializing"},
			wantReason: map[string]string{"p-up": "", "p-init": "awaiting_container"},
			wantUptime: map[string]interface{}{"p-up": float64(111), "p-init": nil},
		},
		{
			// the map-miss case: a pod rest lists that `myself.pods` omits has
			// not been probed, so claiming its container is down would be a
			// guess. `pod get` reports unknown for the same pod.
			name: "a pod missing from the graphql result is unknown, not initializing",
			stub: stub{
				restPods: []map[string]interface{}{restPod("p-miss", "RUNNING", nil)},
				gqlPods:  []map[string]interface{}{},
			},
			wantStatus: map[string]string{"p-miss": "unknown"},
			wantReason: map[string]string{"p-miss": "runtime_unavailable"},
		},
		{
			name: "graphql failure degrades every pod to unknown without failing the list",
			stub: stub{
				restPods:  []map[string]interface{}{restPod("p-up", "RUNNING", nil)},
				gqlStatus: http.StatusInternalServerError,
			},
			wantStatus: map[string]string{"p-up": "unknown"},
			wantReason: map[string]string{"p-up": "runtime_unavailable"},
		},
		{
			// stale telemetry outlives a stopped container: observed live with
			// uptimeInSeconds frozen at its last value on an EXITED pod.
			name: "a stopped pod with stale telemetry is stopped and reports no uptime",
			stub: stub{
				restPods: []map[string]interface{}{
					restPod("p-stop", "EXITED", map[string]interface{}{"lastStatusChange": "Exited by user: x"}),
				},
				gqlPods: []map[string]interface{}{
					gqlPod("p-stop", "EXITED", running, map[string]interface{}{"lastStatusChange": "Exited by user: x"}),
				},
			},
			wantStatus: map[string]string{"p-stop": "stopped"},
			wantReason: map[string]string{"p-stop": "stopped_by_user"},
			wantUptime: map[string]interface{}{"p-stop": nil},
		},
		{
			// the one involuntary stop with a real recorded cause.
			name: "an outbid spot pod is attributed",
			stub: stub{
				restPods: []map[string]interface{}{
					restPod("p-out", "EXITED", map[string]interface{}{"lastStatusChange": "Outbid: Wed Jul 29 2026"}),
				},
				gqlPods: []map[string]interface{}{
					gqlPod("p-out", "EXITED", nil, map[string]interface{}{"lastStatusChange": "Outbid: Wed Jul 29 2026"}),
				},
			},
			wantStatus: map[string]string{"p-out": "stopped"},
			wantReason: map[string]string{"p-out": "stopped_outbid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.stub
			s.start(t)
			resetListFlags(t)

			items := runListJSON(t)
			if len(items) != len(tt.wantStatus) {
				t.Fatalf("expected %d pods, got %d: %v", len(tt.wantStatus), len(items), items)
			}
			for _, item := range items {
				id, _ := item["id"].(string)
				if got, _ := item["runtimeStatus"].(string); got != tt.wantStatus[id] {
					t.Errorf("%s runtimeStatus = %q, want %q", id, got, tt.wantStatus[id])
				}
				if want, ok := tt.wantReason[id]; ok {
					got, _ := item["runtimeStatusReason"].(string)
					if got != want {
						t.Errorf("%s runtimeStatusReason = %q, want %q", id, got, want)
					}
				}
				if want, ok := tt.wantUptime[id]; ok {
					if got := item["uptimeSeconds"]; got != want {
						t.Errorf("%s uptimeSeconds = %v, want %v", id, got, want)
					}
				}
				// the raw text always survives, so a phrasing this cli does not
				// tokenise still reaches the caller.
				if _, ok := item["lastStatusChange"].(string); !ok {
					t.Errorf("%s is missing lastStatusChange: %v", id, item)
				}
			}
		})
	}
}

// TestRunList_SkipsRuntimeCallWhenNothingMatches guards the latency regression:
// `pod list` is polled in loops and never touched graphql before CON-690, so the
// decorative side-call must not be made when there is nothing to decorate.
func TestRunList_SkipsRuntimeCallWhenNothingMatches(t *testing.T) {
	s := &stub{restPods: []map[string]interface{}{restPod("p-stop", "EXITED", nil)}}
	s.start(t)
	resetListFlags(t)
	listAll = false // default: running only, so the exited pod filters out

	items := runListJSON(t)
	if len(items) != 0 {
		t.Fatalf("expected no pods, got %v", items)
	}
	if s.graphqlHits != 0 {
		t.Errorf("graphql was called %d times for an empty result set", s.graphqlHits)
	}
}

// TestRunList_RuntimeCallIsBulk guards against N+1: one graphql request no
// matter how many pods are listed.
func TestRunList_RuntimeCallIsBulk(t *testing.T) {
	s := &stub{}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		s.restPods = append(s.restPods, restPod(id, "RUNNING", nil))
		s.gqlPods = append(s.gqlPods, gqlPod(id, "RUNNING", map[string]interface{}{"uptimeInSeconds": 5}, nil))
	}
	s.start(t)
	resetListFlags(t)

	if items := runListJSON(t); len(items) != 5 {
		t.Fatalf("expected 5 pods, got %d", len(items))
	}
	if s.graphqlHits != 1 {
		t.Errorf("graphql called %d times for 5 pods, want exactly 1", s.graphqlHits)
	}
}

func TestRunGet_RuntimeStatus(t *testing.T) {
	sshPort := map[string]interface{}{
		"ip": "1.2.3.4", "isIpPublic": true, "privatePort": 22, "publicPort": 40022, "type": "tcp",
	}

	tests := []struct {
		name       string
		stub       stub
		id         string
		wantStatus string
		wantReason string
		wantSSHErr string // "" means a real connection is expected
		wantUptime interface{}
	}{
		{
			name: "running with a public 22 yields a connection",
			stub: stub{
				restPods: []map[string]interface{}{restPod("p", "RUNNING", map[string]interface{}{"ports": []string{"22/tcp"}})},
				gqlPods: []map[string]interface{}{gqlPod("p", "RUNNING", map[string]interface{}{
					"uptimeInSeconds": 18, "ports": []interface{}{sshPort},
				}, map[string]interface{}{"ports": "22/tcp"})},
			},
			id:         "p",
			wantStatus: "running",
			wantUptime: float64(18),
		},
		{
			name: "initializing says why instead of a bare not-ready",
			stub: stub{
				restPods: []map[string]interface{}{restPod("p", "RUNNING", nil)},
				gqlPods:  []map[string]interface{}{gqlPod("p", "RUNNING", nil, nil)},
			},
			id:         "p",
			wantStatus: "initializing",
			wantReason: "awaiting_container",
			wantSSHErr: "pod not ready: no container reported yet (image pull, container create or boot)",
			wantUptime: nil,
		},
		{
			// the advice bug: 22 is already declared, so recreating with
			// --ports 22/tcp would change nothing.
			name: "running without a routable 22 does not tell you to recreate a pod that declares it",
			stub: stub{
				restPods: []map[string]interface{}{restPod("p", "RUNNING", map[string]interface{}{"ports": []string{"22/tcp"}})},
				gqlPods: []map[string]interface{}{gqlPod("p", "RUNNING", map[string]interface{}{
					"uptimeInSeconds": 99,
					"ports": []interface{}{map[string]interface{}{
						"ip": "10.0.0.1", "isIpPublic": false, "privatePort": 22, "publicPort": 40022, "type": "http",
					}},
				}, map[string]interface{}{"ports": "22/tcp"})},
			},
			id:         "p",
			wantStatus: "running",
			wantSSHErr: "pod not ready: port 22 is mapped but not publicly routable on this machine",
			wantUptime: float64(99),
		},
		{
			name: "a stopped pod with stale ports does not hand back an ssh command",
			stub: stub{
				restPods: []map[string]interface{}{
					restPod("p", "EXITED", map[string]interface{}{"lastStatusChange": "Exited by user: x"}),
				},
				gqlPods: []map[string]interface{}{gqlPod("p", "EXITED", map[string]interface{}{
					"uptimeInSeconds": 261, "ports": []interface{}{sshPort},
				}, map[string]interface{}{"lastStatusChange": "Exited by user: x"})},
			},
			id:         "p",
			wantStatus: "stopped",
			wantReason: "stopped_by_user",
			wantSSHErr: "pod not ready: pod is stopped; start it with 'runpodctl pod start <pod-id>'",
			wantUptime: nil,
		},
		{
			// snapshot skew: the runtime block belongs to the graphql snapshot,
			// so gating it on rest's desiredStatus lets a stale ssh command
			// through. rest's value is still published as desiredStatus.
			name: "rest and graphql disagreeing does not resurrect a stale ssh command",
			stub: stub{
				restPods: []map[string]interface{}{restPod("p", "RUNNING", nil)},
				gqlPods: []map[string]interface{}{gqlPod("p", "EXITED", map[string]interface{}{
					"uptimeInSeconds": 261, "ports": []interface{}{sshPort},
				}, map[string]interface{}{"lastStatusChange": "Exited by user: x"})},
			},
			id:         "p",
			wantStatus: "stopped",
			wantReason: "stopped_by_user",
			wantSSHErr: "pod not ready: pod is stopped; start it with 'runpodctl pod start <pod-id>'",
			wantUptime: nil,
		},
		{
			name: "a pod graphql does not know about is unknown, not initializing",
			stub: stub{
				restPods: []map[string]interface{}{restPod("p", "RUNNING", nil)},
				gqlPods:  []map[string]interface{}{},
			},
			id:         "p",
			wantStatus: "unknown",
			wantReason: "runtime_unavailable",
			wantSSHErr: "ssh info unavailable",
			wantUptime: nil,
		},
		{
			name: "graphql being down is unknown, not initializing",
			stub: stub{
				restPods:  []map[string]interface{}{restPod("p", "RUNNING", nil)},
				gqlStatus: http.StatusInternalServerError,
			},
			id:         "p",
			wantStatus: "unknown",
			wantReason: "runtime_unavailable",
			wantSSHErr: "ssh info unavailable",
			wantUptime: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.stub
			s.start(t)

			got := runGetJSON(t, tt.id)
			if status, _ := got["runtimeStatus"].(string); status != tt.wantStatus {
				t.Errorf("runtimeStatus = %q, want %q", status, tt.wantStatus)
			}
			reason, _ := got["runtimeStatusReason"].(string)
			if reason != tt.wantReason {
				t.Errorf("runtimeStatusReason = %q, want %q", reason, tt.wantReason)
			}
			if got["uptimeSeconds"] != tt.wantUptime {
				t.Errorf("uptimeSeconds = %v, want %v", got["uptimeSeconds"], tt.wantUptime)
			}

			ssh, ok := got["ssh"].(map[string]interface{})
			if !ok {
				t.Fatalf("ssh block missing: %v", got)
			}
			sshErr, _ := ssh["error"].(string)
			if sshErr != tt.wantSSHErr {
				t.Errorf("ssh.error = %q, want %q", sshErr, tt.wantSSHErr)
			}
			if tt.wantSSHErr == "" {
				if _, ok := ssh["ssh_command"]; !ok {
					t.Errorf("expected an ssh_command for a reachable pod: %v", ssh)
				}
			} else if _, ok := ssh["ssh_command"]; ok {
				t.Errorf("ssh_command must not be offered for an unreachable pod: %v", ssh)
			}
		})
	}
}
