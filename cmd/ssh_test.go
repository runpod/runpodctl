package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
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
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/pods":
			_, _ = w.Write([]byte(`{"pods":` + pods + `,"pagination":{"hasNextPage":false,"nextCursor":null}}`))
		case "/account/ssh-keys":
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_REST_V2_URL", server.URL)
	t.Setenv("RUNPOD_GRAPHQL_URL", "http://graphql.invalid")
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

const sshPortJSON = `{"ip":"1.2.3.4","private":22,"public":40022,"type":"tcp"}`

func TestSSHInfo_RuntimeState(t *testing.T) {
	tests := []struct {
		name       string
		pods       string
		wantErr    string // "" means a real connection is expected
		wantStatus string // asserted on the not-ready payload; connections carry no runtimeStatus
	}{
		{
			name: "running pod yields a connection",
			pods: `[{"id":"p","name":"p","status":"RUNNING","ports":["22/tcp"],"runtime":{"uptime":5,"ports":[` + sshPortJSON + `]}}]`,
		},
		{
			name:       "stopped pod with stale ports is refused with a reason",
			pods:       `[{"id":"p","name":"p","status":"EXITED","ports":["22/tcp"],"runtime":{"uptime":261,"ports":[` + sshPortJSON + `]}}]`,
			wantErr:    "pod not ready: pod is stopped; start it with 'runpodctl pod start p'",
			wantStatus: "stopped",
		},
		{
			name:       "terminated pod with stale ports is refused",
			pods:       `[{"id":"p","name":"p","status":"TERMINATED","ports":["22/tcp"],"runtime":{"ports":[` + sshPortJSON + `]}}]`,
			wantErr:    "pod not ready: pod is terminated",
			wantStatus: "terminated",
		},
		{
			name:       "initializing pod says why",
			pods:       `[{"id":"p","name":"p","status":"STARTING","ports":["22/tcp"],"runtime":null}]`,
			wantErr:    "pod not ready: no container reported yet (image pull, container create or boot)",
			wantStatus: "initializing",
		},
		{
			// the suggested command keeps 8888/http: --ports replaces the list.
			name:       "running pod that never asked for 22 is pointed at pod update, keeping its ports",
			pods:       `[{"id":"p","name":"p","status":"RUNNING","ports":["8888/http"],"runtime":{"ports":[{"ip":"1.2.3.4","private":8888,"public":40088,"type":"tcp"}]}}]`,
			wantErr:    "pod not ready: pod does not publish 22/tcp; add it with 'runpodctl pod update p --ports 8888/http,22/tcp' (--ports replaces the whole list, and changing it may restart the container)",
			wantStatus: "running",
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
			} else {
				if _, ok := got["ssh_command"]; ok {
					t.Errorf("must not offer an ssh_command for an unreachable pod: %v", got)
				}
				if gotStatus, _ := got["runtimeStatus"].(string); gotStatus != tt.wantStatus {
					t.Errorf("runtimeStatus = %q, want %q", gotStatus, tt.wantStatus)
				}
			}
		})
	}
}

// TestSSHConnect_ListSkipsDeadPods covers the no-arg legacy path, which builds
// connections through ListConnections rather than FindPodConnection.
func TestSSHConnect_ListSkipsDeadPods(t *testing.T) {
	sshStub(t, `[
		{"id":"up","name":"up","status":"RUNNING","ports":["22/tcp"],"runtime":{"ports":[`+sshPortJSON+`]}},
		{"id":"down","name":"down","status":"EXITED","ports":["22/tcp"],"runtime":{"ports":[`+sshPortJSON+`]}}
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

func TestSSHCmd_HasForgetCommand(t *testing.T) {
	for _, cmd := range sshCmd.Commands() {
		if cmd.Use == "forget <pod-id>" {
			return
		}
	}
	t.Error("expected ssh forget command to exist")
}

func TestSSHForget_RequiresPodID(t *testing.T) {
	if err := sshForgetCmd.Args(sshForgetCmd, []string{}); err == nil {
		t.Error("expected ssh forget to require a pod id")
	}
	if err := sshForgetCmd.Args(sshForgetCmd, []string{"pod123"}); err != nil {
		t.Errorf("unexpected error for pod id: %v", err)
	}
	if err := sshForgetCmd.Args(sshForgetCmd, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

func sshForgetJSON(t *testing.T, podID string) map[string]interface{} {
	t.Helper()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = write

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("output", "json", "")
	runErr := runSSHForget(cmd, []string{podID})

	os.Stdout = saved
	_ = write.Close()
	out, _ := io.ReadAll(read)
	_ = read.Close()

	if runErr != nil {
		t.Fatalf("ssh forget failed: %v (output: %s)", runErr, out)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("ssh forget output is not json: %v (%s)", err, out)
	}
	return result
}

// TestSSHForget_RemovesOnlyThatPod is the scripted recovery after a stop/start
// changed a pod's host key: the named pod's entry goes and is reported by
// fingerprint, another pod's stays, and a second call is a quiet no-op.
func TestSSHForget_RemovesOnlyThatPod(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".runpod", "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	forgotten := knownhosts.Line([]string{"runpod-pod123"}, signer.PublicKey())
	kept := knownhosts.Line([]string{"runpod-other"}, signer.PublicKey())
	store := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(store, []byte(forgotten+"\n"+kept+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := sshForgetJSON(t, "pod123")
	if result["podId"] != "pod123" || result["forgotten"] != true || result["knownHosts"] != store {
		t.Errorf("unexpected result: %v", result)
	}
	fingerprints, _ := result["fingerprints"].([]interface{})
	if len(fingerprints) != 1 || fingerprints[0] != gossh.FingerprintSHA256(signer.PublicKey()) {
		t.Errorf("fingerprints = %v, want the removed key's", result["fingerprints"])
	}
	raw, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != kept+"\n" {
		t.Errorf("known_hosts = %q, want only the other pod's line", raw)
	}

	again := sshForgetJSON(t, "pod123")
	if again["forgotten"] != false {
		t.Errorf("second forget reported a removal: %v", again)
	}
}
