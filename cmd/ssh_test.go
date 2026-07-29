package cmd

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

func TestSSHInfo_NotDeprecated(t *testing.T) {
	if sshInfoCmd.Deprecated != "" {
		t.Errorf("expected ssh info not to be deprecated")
	}
}

func TestSSHInfo_RequiresPodID(t *testing.T) {
	if err := sshInfoCmd.Args(sshInfoCmd, []string{}); err == nil {
		t.Error("expected ssh info to require a pod id")
	}
	if err := sshInfoCmd.Args(sshInfoCmd, []string{"pod123"}); err != nil {
		t.Errorf("unexpected error for pod id: %v", err)
	}
}

func TestSSHConnect_Deprecated(t *testing.T) {
	if sshConnectCmd.Deprecated == "" {
		t.Errorf("expected ssh connect to be deprecated")
	}
}

func TestSSHConnect_LegacyArgs(t *testing.T) {
	if err := sshConnectCmd.Args(sshConnectCmd, []string{}); err != nil {
		t.Errorf("unexpected error for no args: %v", err)
	}
	if err := sshConnectCmd.Args(sshConnectCmd, []string{"pod123"}); err != nil {
		t.Errorf("unexpected error for pod id: %v", err)
	}
	if err := sshConnectCmd.Args(sshConnectCmd, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func TestSSHCmd_HasInfoCommand(t *testing.T) {
	found := false
	for _, cmd := range sshCmd.Commands() {
		if cmd.Use == "info <pod-id>" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ssh info command to exist")
	}
}

func TestSSHCmd_HasRemoveKeyCommand(t *testing.T) {
	found := false
	for _, cmd := range sshCmd.Commands() {
		if cmd.Use == "remove-key" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ssh remove-key command to exist")
	}
}

func TestSSHConnect_Hidden(t *testing.T) {
	if !sshConnectCmd.Hidden {
		t.Error("expected ssh connect to be hidden")
	}
}

func TestSSHRemoveKey_RequiresIdentifier(t *testing.T) {
	origName := sshKeyName
	origFingerprint := sshKeyFingerprint
	t.Cleanup(func() {
		sshKeyName = origName
		sshKeyFingerprint = origFingerprint
	})

	sshKeyName = ""
	sshKeyFingerprint = ""
	if err := sshRemoveKeyCmd.PreRunE(sshRemoveKeyCmd, nil); err == nil {
		t.Error("expected ssh remove-key to require an identifier")
	}

	sshKeyName = "temp-key"
	if err := sshRemoveKeyCmd.PreRunE(sshRemoveKeyCmd, nil); err != nil {
		t.Errorf("unexpected error for name: %v", err)
	}

	sshKeyName = ""
	sshKeyFingerprint = "SHA256:test"
	if err := sshRemoveKeyCmd.PreRunE(sshRemoveKeyCmd, nil); err != nil {
		t.Errorf("unexpected error for fingerprint: %v", err)
	}
}

// --- ssh info / ssh connect against a stub graphql, so the "do not hand back a
// dead connection" gate is actually constrained. Both call sites build their
// connection from runtime.ports, and a stopped pod keeps reporting those for a
// while (observed live on an EXITED pod), so the gate is the only thing between
// an agent and an ssh command that can never connect.

func sshStub(t *testing.T, pods string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "myPods") {
			_, _ = w.Write([]byte(`{"data":{"myself":{"pods":` + pods + `}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"myself":{"pubKey":""}}}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL+"/graphql")
}

func sshCaptureJSON(t *testing.T, args []string, allowAll bool) map[string]interface{} {
	t.Helper()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = write

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("output", "json", "")
	runErr := runSSHInfoWithArgs(cmd, args, allowAll)

	os.Stdout = saved
	_ = write.Close()
	out, _ := io.ReadAll(read)
	_ = read.Close()

	if runErr != nil {
		t.Fatalf("ssh info failed: %v (output: %s)", runErr, out)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not json: %v\n%s", err, out)
	}
	return got
}

const sshPortJSON = `{"ip":"1.2.3.4","isIpPublic":true,"privatePort":22,"publicPort":40022,"type":"tcp"}`

func TestSSHInfo_RuntimeState(t *testing.T) {
	tests := []struct {
		name       string
		pods       string
		wantErr    string // "" means a real connection is expected
		wantStatus string
	}{
		{
			name:       "running pod yields a connection",
			pods:       `[{"id":"p","name":"p","desiredStatus":"RUNNING","ports":"22/tcp","runtime":{"uptimeInSeconds":5,"ports":[` + sshPortJSON + `]}}]`,
			wantStatus: "running",
		},
		{
			name:    "stopped pod with stale ports is refused with a reason",
			pods:    `[{"id":"p","name":"p","desiredStatus":"EXITED","lastStatusChange":"Exited by user: x","ports":"22/tcp","runtime":{"uptimeInSeconds":261,"ports":[` + sshPortJSON + `]}}]`,
			wantErr: "pod not ready: pod is stopped; start it with 'runpodctl pod start <pod-id>'",
		},
		{
			name:    "terminated pod with stale ports is refused",
			pods:    `[{"id":"p","name":"p","desiredStatus":"TERMINATED","lastStatusChange":"Outbid: x","ports":"22/tcp","runtime":{"ports":[` + sshPortJSON + `]}}]`,
			wantErr: "pod not ready: pod is terminated",
		},
		{
			// unknown is not evidence the pod is down: a connection built from
			// live ports must not be thrown away on the strength of it.
			name:       "a state this cli does not model keeps its live connection",
			pods:       `[{"id":"p","name":"p","desiredStatus":"RESTARTING","ports":"22/tcp","runtime":{"ports":[` + sshPortJSON + `]}}]`,
			wantStatus: "unknown",
		},
		{
			name:    "initializing pod says why",
			pods:    `[{"id":"p","name":"p","desiredStatus":"RUNNING","ports":"22/tcp","runtime":null}]`,
			wantErr: "pod not ready: no container reported yet (image pull, container create or boot)",
		},
		{
			name:    "running pod that never asked for 22 is told to recreate",
			pods:    `[{"id":"p","name":"p","desiredStatus":"RUNNING","ports":"8888/http","runtime":{"ports":[{"ip":"1.2.3.4","isIpPublic":true,"privatePort":8888,"publicPort":40088,"type":"tcp"}]}}]`,
			wantErr: "pod not ready: pod does not publish 22/tcp; recreate it with --ports 22/tcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sshStub(t, tt.pods)
			got := sshCaptureJSON(t, []string{"p"}, false)

			gotErr, _ := got["error"].(string)
			if gotErr != tt.wantErr {
				t.Errorf("error = %q, want %q", gotErr, tt.wantErr)
			}
			if tt.wantErr == "" {
				if _, ok := got["ssh_command"]; !ok {
					t.Errorf("expected an ssh_command, got %v", got)
				}
			} else if _, ok := got["ssh_command"]; ok {
				t.Errorf("must not offer an ssh_command for an unreachable pod: %v", got)
			}
		})
	}
}

// TestSSHConnect_ListSkipsDeadPods covers the no-arg legacy path, which builds
// connections through ListConnections rather than FindPodConnection.
func TestSSHConnect_ListSkipsDeadPods(t *testing.T) {
	sshStub(t, `[
		{"id":"up","name":"up","desiredStatus":"RUNNING","ports":"22/tcp","runtime":{"ports":[`+sshPortJSON+`]}},
		{"id":"down","name":"down","desiredStatus":"EXITED","lastStatusChange":"Exited by user: x","ports":"22/tcp","runtime":{"ports":[`+sshPortJSON+`]}}
	]`)

	got := sshCaptureJSON(t, nil, true)
	conns, ok := got["connections"].([]interface{})
	if !ok {
		t.Fatalf("connections missing: %v", got)
	}
	if len(conns) != 1 {
		t.Fatalf("expected only the running pod, got %d: %v", len(conns), conns)
	}
	if first, _ := conns[0].(map[string]interface{}); first["id"] != "up" {
		t.Errorf("listed the wrong pod: %v", conns[0])
	}
}
