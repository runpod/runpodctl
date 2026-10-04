package pod

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// These tests drive `pod list` and `pod get` end to end against a stub rest v2
// control plane, which is the only way the runtimeStatus wiring is actually
// constrained: the derivation itself is unit-tested in internal/podstate, but
// the bugs worth catching live in how the two commands feed it (the desiredStatus
// mapping from v2's status, whether stale ports are gated, which ports count as
// publicly routable). The e2e suite is `//go:build e2e` so CI never runs it.

// stub is a fake rest v2 control plane: /pods, /pods/{id}, /account/ssh-keys.
type stub struct {
	pods     []map[string]interface{}
	requests []string
}

func (s *stub) start(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		s.requests = append(s.requests, r.URL.Path)

		switch {
		case r.URL.Path == "/account/ssh-keys":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []string{}})
		case r.URL.Path == "/pods":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"pods":       s.pods,
				"pagination": map[string]interface{}{"hasNextPage": false, "nextCursor": nil},
			})
		case strings.HasPrefix(r.URL.Path, "/pods/"):
			id := strings.TrimPrefix(r.URL.Path, "/pods/")
			for _, p := range s.pods {
				if p["id"] == id {
					_ = json.NewEncoder(w).Encode(p)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"title": "Not Found", "status": 404, "detail": "pod not found"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", "http://v1.invalid")
	t.Setenv("RUNPOD_REST_V2_URL", server.URL)
	t.Setenv("RUNPOD_GRAPHQL_URL", "http://graphql.invalid")
}

// v2Pod builds a rest v2 pod. runtime is nil for "no container reported".
func v2Pod(id, status string, runtime map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	p := map[string]interface{}{
		"id":      id,
		"name":    id + "-name",
		"status":  status,
		"image":   "img:1",
		"gpu":     map[string]interface{}{"id": "NVIDIA A40", "count": 1, "vcpuCount": 8, "memory": 32},
		"runtime": runtime,
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// v2Port is a runtime port mapping as v2 reports it.
func v2Port(ip string, private, public int, kind string) map[string]interface{} {
	return map[string]interface{}{"ip": ip, "private": private, "public": public, "type": kind}
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
	running := map[string]interface{}{"uptime": 111}

	tests := []struct {
		name        string
		pods        []map[string]interface{}
		wantStatus  map[string]string
		wantReason  map[string]string
		wantUptime  map[string]interface{}
		wantDesired map[string]string
	}{
		{
			name: "telemetry present is running, absent is initializing",
			pods: []map[string]interface{}{
				v2Pod("p-up", "RUNNING", running, nil),
				v2Pod("p-init", "RUNNING", nil, nil),
			},
			wantStatus: map[string]string{"p-up": "running", "p-init": "initializing"},
			wantReason: map[string]string{"p-up": "", "p-init": "awaiting_container"},
			wantUptime: map[string]interface{}{"p-up": float64(111), "p-init": nil},
		},
		{
			// v2's live lifecycle states are printed as the requested state
			// desiredStatus has always carried
			// ERROR is unrecoverable, so it is not folded into RUNNING
			name: "provisioning and starting pods were asked to run; error pods are not",
			pods: []map[string]interface{}{
				v2Pod("p-prov", "PROVISIONING", nil, nil),
				v2Pod("p-start", "STARTING", nil, nil),
				v2Pod("p-err", "ERROR", nil, nil),
			},
			wantStatus:  map[string]string{"p-prov": "initializing", "p-start": "initializing", "p-err": "unknown"},
			wantDesired: map[string]string{"p-prov": "RUNNING", "p-start": "RUNNING", "p-err": "ERROR"},
		},
		{
			// stale telemetry outlives a stopped container: observed live with
			// the uptime frozen at its last value on an EXITED pod.
			name: "a stopped pod with stale telemetry is stopped and reports no uptime",
			pods: []map[string]interface{}{
				v2Pod("p-stop", "EXITED", running, nil),
			},
			wantStatus:  map[string]string{"p-stop": "stopped"},
			wantReason:  map[string]string{"p-stop": ""},
			wantUptime:  map[string]interface{}{"p-stop": nil},
			wantDesired: map[string]string{"p-stop": "EXITED"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stub{pods: tt.pods}
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
					if got, _ := item["runtimeStatusReason"].(string); got != want {
						t.Errorf("%s runtimeStatusReason = %q, want %q", id, got, want)
					}
				}
				if want, ok := tt.wantUptime[id]; ok {
					if got := item["uptimeSeconds"]; got != want {
						t.Errorf("%s uptimeSeconds = %v, want %v", id, got, want)
					}
				}
				if want, ok := tt.wantDesired[id]; ok {
					if got, _ := item["desiredStatus"].(string); got != want {
						t.Errorf("%s desiredStatus = %q, want %q", id, got, want)
					}
				}
			}
			// one read: status and telemetry share a snapshot, and nothing else
			// is called
			if len(s.requests) != 1 || s.requests[0] != "/pods" {
				t.Errorf("requests = %v, want exactly one GET /pods", s.requests)
			}
		})
	}
}

func TestRunGet_RuntimeStatus(t *testing.T) {
	sshPort := v2Port("1.2.3.4", 22, 40022, "tcp")

	tests := []struct {
		name       string
		pod        map[string]interface{}
		wantStatus string
		wantReason string
		wantSSHErr string // "" means a real connection is expected
		wantUptime interface{}
	}{
		{
			name: "running with a public 22 yields a connection",
			pod: v2Pod("p", "RUNNING", map[string]interface{}{
				"uptime": 18, "ports": []interface{}{sshPort},
			}, map[string]interface{}{"ports": []string{"22/tcp"}}),
			wantStatus: "running",
			wantUptime: float64(18),
		},
		{
			name:       "initializing says why instead of a bare not-ready",
			pod:        v2Pod("p", "STARTING", nil, nil),
			wantStatus: "initializing",
			wantReason: "awaiting_container",
			wantSSHErr: "pod not ready: no container reported yet (image pull, container create or boot)",
			wantUptime: nil,
		},
		{
			// v2 has no isIpPublic: an address on the proxy's carrier-grade nat
			// range is not routable, so 22 there is not an ssh endpoint
			name: "running with 22 only on a private address is not reachable",
			pod: v2Pod("p", "RUNNING", map[string]interface{}{
				"uptime": 99, "ports": []interface{}{v2Port("100.65.17.95", 22, 60476, "tcp")},
			}, map[string]interface{}{"ports": []string{"22/tcp"}}),
			wantStatus: "running",
			wantSSHErr: "pod not ready: port 22 is mapped but not publicly routable on this machine",
			wantUptime: float64(99),
		},
		{
			name: "a stopped pod with stale ports does not hand back an ssh command",
			pod: v2Pod("p", "EXITED", map[string]interface{}{
				"uptime": 261, "ports": []interface{}{sshPort},
			}, nil),
			wantStatus: "stopped",
			wantSSHErr: "pod not ready: pod is stopped; start it with 'runpodctl pod start p'",
			wantUptime: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stub{pods: []map[string]interface{}{tt.pod}}
			s.start(t)

			got := runGetJSON(t, "p")
			if status, _ := got["runtimeStatus"].(string); status != tt.wantStatus {
				t.Errorf("runtimeStatus = %q, want %q", status, tt.wantStatus)
			}
			if reason, _ := got["runtimeStatusReason"].(string); reason != tt.wantReason {
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

// v2's RFC 3339 createdAt is re-rendered in v1's layout; --since must still
// parse it, or every pod is filtered out
func TestRunList_SinceFiltersOnV2CreatedAt(t *testing.T) {
	recent := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
	old := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339Nano)
	s := &stub{pods: []map[string]interface{}{
		v2Pod("p-new", "RUNNING", nil, map[string]interface{}{"createdAt": recent}),
		v2Pod("p-old", "RUNNING", nil, map[string]interface{}{"createdAt": old}),
	}}
	s.start(t)
	resetListFlags(t)
	listSince = "1h"

	items := runListJSON(t)
	if len(items) != 1 || items[0]["id"] != "p-new" {
		t.Fatalf("--since 1h = %v, want only p-new", items)
	}
	if created, _ := items[0]["createdAt"].(string); created == "" {
		t.Errorf("createdAt missing: %v", items[0])
	}
}
