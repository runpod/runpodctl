//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// deletePodOnCleanup registers a delete so a pod cannot outlive the test. Pods
// bill by the second, so this runs even when the assertions fail.
func deletePodOnCleanup(t *testing.T, podID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, stderr, err := runCLI("pod", "delete", podID); err != nil {
			t.Errorf("failed to delete pod %s: %v\nstderr: %s", podID, err, stderr)
		} else {
			t.Logf("cleaned up pod %s", podID)
		}
	})
}

func decodeErrorObject(t *testing.T, stderr string) map[string]interface{} {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	last := lines[len(lines)-1]
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(last), &obj); err != nil {
		t.Fatalf("last stderr line is not a json error object: %v\nstderr: %s", err, stderr)
	}
	return obj
}

// TestCLI_PodCreateWaitTimeout exercises the timeout path on the cheapest thing
// that can never satisfy it: a cpu pod running an image with no sshd. Prod does
// allocate a public port 22 for it, which is exactly the case the wait must not
// mistake for readiness.
func TestCLI_PodCreateWaitTimeout(t *testing.T) {
	name := "e2e-wait-timeout-" + time.Now().Format("20060102150405")
	stdout, stderr, err := runCLI("pod", "create",
		"--compute-type", "cpu",
		"--image", "alpine:3.20",
		"--docker-args", "sleep infinity",
		"--container-disk-in-gb", "5",
		"--volume-in-gb", "0",
		"--name", name,
		"--wait", "--wait-timeout", "25s")

	if err == nil {
		// it should not have become reachable; still clean up whatever exists.
		var pod map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(stdout), &pod); jsonErr == nil {
			if podID, ok := pod["id"].(string); ok && podID != "" {
				deletePodOnCleanup(t, podID)
			}
		}
		t.Fatalf("expected a timeout for an image with no sshd, got success:\n%s", stdout)
	}

	if strings.Contains(strings.ToLower(stderr), "not supported") || strings.Contains(strings.ToLower(stderr), "not enabled") {
		t.Skipf("cpu pods not available for this account: %s", strings.TrimSpace(stderr))
	}

	// stdout must stay empty: a failed wait emits no payload at all, so a caller
	// piping stdout into a parser never sees a half-created resource.
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout must stay empty on a failed wait, got: %q", stdout)
	}

	obj := decodeErrorObject(t, stderr)
	if code, _ := obj["code"].(string); code != "wait_timeout" {
		t.Errorf("code = %v, want wait_timeout (stderr: %s)", obj["code"], stderr)
	}

	message, _ := obj["error"].(string)
	if !strings.Contains(message, "last known state") {
		t.Errorf("the error must carry the last known state: %q", message)
	}

	// the pod id has to be in the error, because the pod was created and bills.
	podID := podIDFromWaitError(message)
	if podID == "" {
		t.Fatalf("the error must name the created pod: %q", message)
	}
	deletePodOnCleanup(t, podID)

	if !strings.Contains(stderr, "cpu pods are created through the rest api") {
		t.Errorf("expected the cpu ssh caveat on stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "waiting for ssh on pod "+podID) {
		t.Errorf("expected progress on stderr: %s", stderr)
	}
}

// podIDFromWaitError pulls the pod id out of "... pod <id> was created ...".
func podIDFromWaitError(message string) string {
	const marker = "pod "
	idx := strings.Index(message, "; pod ")
	if idx < 0 {
		return ""
	}
	rest := message[idx+2+len(marker):]
	end := strings.Index(rest, " ")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// TestCLI_PodCreateWait proves the success path: --wait blocks until ssh really
// answers and then prints one json object carrying the live ssh command.
// Secure cloud on purpose — a community pod without --public-ip never gets a
// publicly mapped port 22, so there would be nothing to connect to.
func TestCLI_PodCreateWait(t *testing.T) {
	name := "e2e-wait-ssh-" + time.Now().Format("20060102150405")
	stdout, stderr, err := runCLI("pod", "create",
		"--template-id", "runpod-torch-v21",
		"--gpu-id", "NVIDIA RTX A4000",
		"--cloud-type", "SECURE",
		"--volume-in-gb", "0",
		"--name", name,
		"--wait", "--wait-timeout", "8m")
	if err != nil {
		if shouldSkipCommunityCreate(stdout + stderr) {
			t.Skipf("no capacity for the test gpu: %s", strings.TrimSpace(stderr))
		}
		// a timeout still created the pod; delete it before failing.
		if podID := podIDFromWaitError(stderr); podID != "" {
			deletePodOnCleanup(t, podID)
		}
		t.Fatalf("pod create --wait failed: %v\nstderr: %s", err, stderr)
	}

	var pod map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(stdout), &pod); jsonErr != nil {
		t.Fatalf("stdout must be exactly one json object: %v\nstdout: %s", jsonErr, stdout)
	}
	podID, _ := pod["id"].(string)
	if podID == "" {
		t.Fatalf("expected a pod id in the payload: %s", stdout)
	}
	deletePodOnCleanup(t, podID)

	// --wait prints the `pod get` shape, so the ssh block is present and filled
	// in: that is the whole reason to wait rather than take the create response.
	sshInfo, ok := pod["ssh"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected an ssh block in the --wait payload: %s", stdout)
	}
	if command, _ := sshInfo["ssh_command"].(string); !strings.HasPrefix(command, "ssh ") {
		t.Errorf("expected a usable ssh command, got %v", sshInfo["ssh_command"])
	}
	if port, _ := sshInfo["port"].(float64); port <= 0 {
		t.Errorf("expected a public ssh port, got %v", sshInfo["port"])
	}

	if !strings.Contains(stderr, "ready after") {
		t.Errorf("expected the readiness line on stderr: %s", stderr)
	}
}

// TestCLI_ServerlessCreateWaitRequiresWarmWorker costs nothing: the refusal has
// to happen before the endpoint is created.
func TestCLI_ServerlessCreateWaitRequiresWarmWorker(t *testing.T) {
	before := listEndpointIDs(t)

	stdout, stderr, err := runCLI("serverless", "create",
		"--template-id", "e2e-does-not-exist",
		"--gpu-id", "NVIDIA RTX A5000",
		"--wait")
	if err == nil {
		t.Fatalf("expected --wait to be refused at workers-min 0, got: %s", stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout must stay empty, got %q", stdout)
	}
	if !strings.Contains(stderr, "--wait needs --workers-min 1 or more") {
		t.Errorf("expected the workers-min explanation, got: %s", stderr)
	}

	after := listEndpointIDs(t)
	if len(after) != len(before) {
		t.Errorf("no endpoint may be created: %v -> %v", before, after)
	}
}

func listEndpointIDs(t *testing.T) []string {
	t.Helper()
	stdout, stderr, err := runCLI("serverless", "list")
	if err != nil {
		t.Fatalf("failed to list endpoints: %v\nstderr: %s", err, stderr)
	}
	var endpoints []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &endpoints); err != nil {
		t.Fatalf("endpoint list is not json: %v\n%s", err, stdout)
	}
	ids := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		id, _ := endpoint["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestCLI_ServerlessCreateWait creates a warm worker, so it bills for as long as
// the endpoint exists: the endpoint and its template are torn down in Cleanup,
// deepest first.
func TestCLI_ServerlessCreateWait(t *testing.T) {
	suffix := time.Now().Format("20060102150405")

	stdout, stderr, err := runCLI("template", "create",
		"--name", "e2e-wait-tpl-"+suffix,
		"--image", "runpod/serverless-hello-world:latest",
		"--serverless",
		"--container-disk-in-gb", "5",
		"--volume-in-gb", "0")
	if err != nil {
		t.Fatalf("failed to create the test template: %v\nstderr: %s", err, stderr)
	}
	var template map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &template); err != nil {
		t.Fatalf("template create output is not json: %v\n%s", err, stdout)
	}
	templateID, _ := template["id"].(string)
	if templateID == "" {
		t.Fatalf("expected a template id: %s", stdout)
	}
	t.Cleanup(func() {
		if _, cleanupStderr, cleanupErr := runCLI("template", "delete", templateID); cleanupErr != nil {
			t.Errorf("failed to delete template %s: %v\nstderr: %s", templateID, cleanupErr, cleanupStderr)
		}
	})

	stdout, stderr, err = runCLI("serverless", "create",
		"--template-id", templateID,
		"--gpu-id", "NVIDIA RTX A5000",
		"--name", "e2e-wait-ep-"+suffix,
		"--workers-min", "1",
		"--workers-max", "1",
		"--wait", "--wait-timeout", "7m")

	endpointID := ""
	var endpoint map[string]interface{}
	if jsonErr := json.Unmarshal([]byte(stdout), &endpoint); jsonErr == nil {
		endpointID, _ = endpoint["id"].(string)
	} else if err != nil {
		endpointID = endpointIDFromWaitError(stderr)
	}
	if endpointID != "" {
		t.Cleanup(func() {
			if _, cleanupStderr, cleanupErr := runCLI("serverless", "delete", endpointID); cleanupErr != nil {
				t.Errorf("failed to delete endpoint %s: %v\nstderr: %s", endpointID, cleanupErr, cleanupStderr)
			}
		})
	}

	if err != nil {
		if shouldSkipCommunityCreate(stdout + stderr) {
			t.Skipf("no serverless capacity for the test gpu: %s", strings.TrimSpace(stderr))
		}
		t.Fatalf("serverless create --wait failed: %v\nstderr: %s", err, stderr)
	}
	if endpointID == "" {
		t.Fatalf("expected an endpoint id in the payload: %s", stdout)
	}

	// --wait does not change the payload for endpoints: the create response is
	// the same one you get without it (the config does not change while workers
	// boot), so only the timing differs.
	if urls, ok := endpoint["urls"].(map[string]interface{}); !ok || urls["health"] == "" {
		t.Errorf("expected the invoke urls in the payload: %s", stdout)
	}
	if !strings.Contains(stderr, "ready after") {
		t.Errorf("expected the readiness line on stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "workers ready") {
		t.Errorf("expected the worker counts in the progress note: %s", stderr)
	}
}

// endpointIDFromWaitError pulls the id out of "... endpoint <id> was created ...".
func endpointIDFromWaitError(message string) string {
	idx := strings.Index(message, "; endpoint ")
	if idx < 0 {
		return ""
	}
	rest := message[idx+len("; endpoint "):]
	end := strings.Index(rest, " ")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
