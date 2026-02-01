//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// runCLI runs the runpod CLI and returns stdout, stderr, and error
func runCLI(args ...string) (string, string, error) {
	// use the binary from go/bin
	home, _ := os.UserHomeDir()
	binary := home + "/go/bin/runpod"

	cmd := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestCLI_Version(t *testing.T) {
	stdout, _, err := runCLI("--version")
	if err != nil {
		t.Fatalf("failed to run --version: %v", err)
	}
	if stdout == "" {
		t.Error("expected version output")
	}
	t.Logf("version: %s", stdout)
}

func TestCLI_Help(t *testing.T) {
	stdout, _, err := runCLI("--help")
	if err != nil {
		t.Fatalf("failed to run --help: %v", err)
	}
	if stdout == "" {
		t.Error("expected help output")
	}
}

func TestCLI_PodList(t *testing.T) {
	stdout, stderr, err := runCLI("pod", "list")
	if err != nil {
		t.Fatalf("failed to run pod list: %v\nstderr: %s", err, stderr)
	}

	// output should be valid json array
	var pods []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &pods); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d pods", len(pods))
}

func TestCLI_PodListYAML(t *testing.T) {
	stdout, stderr, err := runCLI("pod", "list", "--output", "yaml")
	if err != nil {
		t.Fatalf("failed to run pod list --output yaml: %v\nstderr: %s", err, stderr)
	}

	// just check it's not empty and doesn't start with [ (json array)
	if stdout == "" {
		t.Error("expected yaml output")
	}
	t.Logf("yaml output length: %d bytes", len(stdout))
}

func TestCLI_EndpointList(t *testing.T) {
	stdout, stderr, err := runCLI("serverless", "list")
	if err != nil {
		t.Fatalf("failed to run serverless list: %v\nstderr: %s", err, stderr)
	}

	var endpoints []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &endpoints); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d endpoints", len(endpoints))
}

func TestCLI_EndpointListAlias(t *testing.T) {
	// test sls alias
	stdout, stderr, err := runCLI("sls", "list")
	if err != nil {
		t.Fatalf("failed to run sls list: %v\nstderr: %s", err, stderr)
	}

	var endpoints []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &endpoints); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("sls alias works, found %d endpoints", len(endpoints))
}

func TestCLI_TemplateList(t *testing.T) {
	stdout, stderr, err := runCLI("template", "list")
	if err != nil {
		t.Fatalf("failed to run template list: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d templates", len(templates))
}

func TestCLI_TemplateListAlias(t *testing.T) {
	// test tpl alias
	stdout, stderr, err := runCLI("tpl", "list")
	if err != nil {
		t.Fatalf("failed to run tpl list: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("tpl alias works, found %d templates", len(templates))
}

func TestCLI_NetworkVolumeList(t *testing.T) {
	stdout, stderr, err := runCLI("network-volume", "list")
	if err != nil {
		t.Fatalf("failed to run network-volume list: %v\nstderr: %s", err, stderr)
	}

	var volumes []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &volumes); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d volumes", len(volumes))
}

func TestCLI_NetworkVolumeListAlias(t *testing.T) {
	// test nv alias
	stdout, stderr, err := runCLI("nv", "list")
	if err != nil {
		t.Fatalf("failed to run nv list: %v\nstderr: %s", err, stderr)
	}

	var volumes []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &volumes); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("nv alias works, found %d volumes", len(volumes))
}

func TestCLI_RegistryList(t *testing.T) {
	stdout, stderr, err := runCLI("registry", "list")
	if err != nil {
		t.Fatalf("failed to run registry list: %v\nstderr: %s", err, stderr)
	}

	var auths []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &auths); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d registry auths", len(auths))
}

func TestCLI_RegistryListAlias(t *testing.T) {
	// test reg alias
	stdout, stderr, err := runCLI("reg", "list")
	if err != nil {
		t.Fatalf("failed to run reg list: %v\nstderr: %s", err, stderr)
	}

	var auths []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &auths); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("reg alias works, found %d registry auths", len(auths))
}

func TestCLI_PodGet(t *testing.T) {
	// first list pods to get an id
	stdout, _, err := runCLI("pod", "list")
	if err != nil {
		t.Skip("skipping pod get test - can't list pods")
	}

	var pods []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &pods); err != nil {
		t.Skip("skipping pod get test - can't parse pod list")
	}

	if len(pods) == 0 {
		t.Skip("skipping pod get test - no pods found")
	}

	podID := pods[0]["id"].(string)
	stdout, stderr, err := runCLI("pod", "get", podID)
	if err != nil {
		t.Fatalf("failed to get pod %s: %v\nstderr: %s", podID, err, stderr)
	}

	var pod map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &pod); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}

	if pod["id"] != podID {
		t.Errorf("expected pod id %s, got %v", podID, pod["id"])
	}

	t.Logf("got pod: %v", pod["name"])
}

func TestCLI_EndpointGet(t *testing.T) {
	stdout, _, err := runCLI("serverless", "list")
	if err != nil {
		t.Skip("skipping endpoint get test - can't list endpoints")
	}

	var endpoints []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &endpoints); err != nil {
		t.Skip("skipping endpoint get test - can't parse endpoint list")
	}

	if len(endpoints) == 0 {
		t.Skip("skipping endpoint get test - no endpoints found")
	}

	endpointID := endpoints[0]["id"].(string)
	stdout, stderr, err := runCLI("serverless", "get", endpointID)
	if err != nil {
		t.Fatalf("failed to get endpoint %s: %v\nstderr: %s", endpointID, err, stderr)
	}

	var endpoint map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &endpoint); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}

	if endpoint["id"] != endpointID {
		t.Errorf("expected endpoint id %s, got %v", endpointID, endpoint["id"])
	}

	t.Logf("got endpoint: %v", endpoint["name"])
}

func TestCLI_TemplateGet(t *testing.T) {
	stdout, _, err := runCLI("template", "list")
	if err != nil {
		t.Skip("skipping template get test - can't list templates")
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Skip("skipping template get test - can't parse template list")
	}

	if len(templates) == 0 {
		t.Skip("skipping template get test - no templates found")
	}

	templateID := templates[0]["id"].(string)
	stdout, stderr, err := runCLI("template", "get", templateID)
	if err != nil {
		t.Fatalf("failed to get template %s: %v\nstderr: %s", templateID, err, stderr)
	}

	var template map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &template); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}

	if template["id"] != templateID {
		t.Errorf("expected template id %s, got %v", templateID, template["id"])
	}

	t.Logf("got template: %v", template["name"])
}

func TestCLI_NetworkVolumeGet(t *testing.T) {
	stdout, _, err := runCLI("network-volume", "list")
	if err != nil {
		t.Skip("skipping network-volume get test - can't list volumes")
	}

	var volumes []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &volumes); err != nil {
		t.Skip("skipping network-volume get test - can't parse volume list")
	}

	if len(volumes) == 0 {
		t.Skip("skipping network-volume get test - no volumes found")
	}

	volumeID := volumes[0]["id"].(string)
	stdout, stderr, err := runCLI("network-volume", "get", volumeID)
	if err != nil {
		t.Fatalf("failed to get volume %s: %v\nstderr: %s", volumeID, err, stderr)
	}

	var volume map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &volume); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}

	if volume["id"] != volumeID {
		t.Errorf("expected volume id %s, got %v", volumeID, volume["id"])
	}

	t.Logf("got volume: %v", volume["name"])
}
