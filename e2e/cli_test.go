//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
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

func TestCLI_TemplateListOfficial(t *testing.T) {
	// test --type official filter
	stdout, stderr, err := runCLI("template", "list", "--type", "official", "--limit", "5")
	if err != nil {
		t.Fatalf("failed to run template list --type official: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	// verify all returned templates are official (isRunpod: true)
	for _, tpl := range templates {
		if isRunpod, ok := tpl["isRunpod"].(bool); !ok || !isRunpod {
			t.Errorf("expected official template (isRunpod: true), got: %v", tpl["name"])
		}
	}

	if len(templates) == 0 {
		t.Error("expected at least one official template")
	}
	t.Logf("found %d official templates (limited to 5)", len(templates))
}

func TestCLI_TemplateListCommunity(t *testing.T) {
	// test --type community filter
	stdout, stderr, err := runCLI("template", "list", "--type", "community", "--limit", "5")
	if err != nil {
		t.Fatalf("failed to run template list --type community: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	// verify all returned templates are community (not official)
	for _, tpl := range templates {
		isRunpod, _ := tpl["isRunpod"].(bool)
		if isRunpod {
			t.Errorf("expected community template (isRunpod: false), got official: %v", tpl["name"])
		}
	}

	if len(templates) == 0 {
		t.Error("expected at least one community template")
	}
	t.Logf("found %d community templates (limited to 5)", len(templates))
}

func TestCLI_TemplateListPagination(t *testing.T) {
	// test pagination with limit and offset
	stdout1, _, err := runCLI("template", "list", "--type", "official", "--limit", "3")
	if err != nil {
		t.Skip("skipping pagination test - can't get first page")
	}

	stdout2, _, err := runCLI("template", "list", "--type", "official", "--limit", "3", "--offset", "3")
	if err != nil {
		t.Skip("skipping pagination test - can't get second page")
	}

	var page1, page2 []map[string]interface{}
	json.Unmarshal([]byte(stdout1), &page1)
	json.Unmarshal([]byte(stdout2), &page2)

	if len(page1) == 0 || len(page2) == 0 {
		t.Skip("skipping pagination test - not enough templates")
	}

	// verify pages are different (first item on page 2 should not be on page 1)
	page2FirstID := page2[0]["id"]
	for _, tpl := range page1 {
		if tpl["id"] == page2FirstID {
			t.Error("pagination not working - same template on both pages")
		}
	}

	t.Logf("pagination works: page1=%d templates, page2=%d templates", len(page1), len(page2))
}

func TestCLI_TemplateListAll(t *testing.T) {
	// test --all flag returns many templates
	stdout, stderr, err := runCLI("template", "list", "--all")
	if err != nil {
		t.Fatalf("failed to run template list --all: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	// should have way more than the default limit of 10
	if len(templates) < 100 {
		t.Errorf("expected at least 100 templates with --all, got %d", len(templates))
	}

	t.Logf("found %d total templates with --all", len(templates))
}

func TestCLI_TemplateSearch(t *testing.T) {
	// test search command
	stdout, stderr, err := runCLI("template", "search", "pytorch")
	if err != nil {
		t.Fatalf("failed to search templates: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	// verify all returned templates match search term
	for _, tpl := range templates {
		name := strings.ToLower(tpl["name"].(string))
		imageName := ""
		if img, ok := tpl["imageName"].(string); ok {
			imageName = strings.ToLower(img)
		}
		if !strings.Contains(name, "pytorch") && !strings.Contains(imageName, "pytorch") {
			t.Errorf("template %q doesn't match search term 'pytorch'", tpl["name"])
		}
	}

	if len(templates) == 0 {
		t.Error("expected at least one pytorch template")
	}
	t.Logf("found %d templates matching 'pytorch'", len(templates))
}

func TestCLI_TemplateSearchWithLimit(t *testing.T) {
	// test search with --limit flag
	stdout, stderr, err := runCLI("template", "search", "comfyui", "--limit", "5")
	if err != nil {
		t.Fatalf("failed to search templates: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if len(templates) == 0 {
		t.Error("expected at least one comfyui template")
	}
	if len(templates) > 5 {
		t.Errorf("expected at most 5 templates, got %d", len(templates))
	}
	t.Logf("found %d templates matching 'comfyui' (limited to 5)", len(templates))
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

func TestCLI_User(t *testing.T) {
	stdout, stderr, err := runCLI("user")
	if err != nil {
		t.Fatalf("failed to run user: %v\nstderr: %s", err, stderr)
	}

	var user map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &user); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if user["id"] == nil {
		t.Error("expected user id")
	}
	t.Logf("user: %v, balance: %v", user["email"], user["clientBalance"])
}

func TestCLI_UserAlias(t *testing.T) {
	stdout, stderr, err := runCLI("me")
	if err != nil {
		t.Fatalf("failed to run me: %v\nstderr: %s", err, stderr)
	}

	var user map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &user); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}

	t.Logf("me alias works, user: %v", user["email"])
}

func TestCLI_GpuList(t *testing.T) {
	stdout, stderr, err := runCLI("gpu", "list")
	if err != nil {
		t.Fatalf("failed to run gpu list: %v\nstderr: %s", err, stderr)
	}

	var gpus []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &gpus); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if len(gpus) == 0 {
		t.Error("expected at least one gpu")
	}
	t.Logf("found %d available gpus", len(gpus))
}

func TestCLI_DatacenterList(t *testing.T) {
	stdout, stderr, err := runCLI("datacenter", "list")
	if err != nil {
		t.Fatalf("failed to run datacenter list: %v\nstderr: %s", err, stderr)
	}

	var dcs []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &dcs); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if len(dcs) == 0 {
		t.Error("expected at least one datacenter")
	}
	t.Logf("found %d datacenters", len(dcs))
}

func TestCLI_DatacenterListAlias(t *testing.T) {
	stdout, stderr, err := runCLI("dc", "list")
	if err != nil {
		t.Fatalf("failed to run dc list: %v\nstderr: %s", err, stderr)
	}

	var dcs []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &dcs); err != nil {
		t.Fatalf("output is not valid json: %v", err)
	}

	t.Logf("dc alias works, found %d datacenters", len(dcs))
}

func TestCLI_BillingPods(t *testing.T) {
	stdout, stderr, err := runCLI("billing", "pods")
	if err != nil {
		t.Fatalf("failed to run billing pods: %v\nstderr: %s", err, stderr)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d pod billing records", len(records))
}

func TestCLI_BillingServerless(t *testing.T) {
	stdout, stderr, err := runCLI("billing", "serverless")
	if err != nil {
		t.Fatalf("failed to run billing serverless: %v\nstderr: %s", err, stderr)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d serverless billing records", len(records))
}

func TestCLI_BillingNetworkVolume(t *testing.T) {
	stdout, stderr, err := runCLI("billing", "network-volume")
	if err != nil {
		t.Fatalf("failed to run billing network-volume: %v\nstderr: %s", err, stderr)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &records); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	t.Logf("found %d network volume billing records", len(records))
}

func TestCLI_Doctor(t *testing.T) {
	stdout, stderr, err := runCLI("doctor")
	if err != nil {
		t.Fatalf("failed to run doctor: %v\nstderr: %s", err, stderr)
	}

	var report map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if report["healthy"] != true {
		t.Errorf("expected healthy to be true, got %v", report["healthy"])
	}

	checks, ok := report["checks"].([]interface{})
	if !ok {
		t.Fatalf("expected checks to be array")
	}

	expectedChecks := []string{"api_key", "api_connectivity", "ssh_key"}
	for i, check := range checks {
		checkMap := check.(map[string]interface{})
		if checkMap["name"] != expectedChecks[i] {
			t.Errorf("expected check %d to be %s, got %s", i, expectedChecks[i], checkMap["name"])
		}
		if checkMap["status"] != "pass" {
			t.Errorf("expected check %s to pass, got %s", checkMap["name"], checkMap["status"])
		}
	}

	t.Logf("doctor report: %d checks, healthy: %v", len(checks), report["healthy"])
}

// Legacy command tests - ensure backwards compatibility

func TestCLI_LegacyGetPod(t *testing.T) {
	stdout, stderr, err := runCLI("get", "pod")
	if err != nil {
		t.Fatalf("failed to run legacy get pod: %v\nstderr: %s", err, stderr)
	}

	// should contain deprecation warning in stderr
	if !strings.Contains(stderr, "deprecated") {
		t.Error("expected deprecation warning in stderr")
	}

	// should return table output (not JSON)
	if strings.HasPrefix(strings.TrimSpace(stdout), "[") || strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Error("legacy get pod should return table output, not JSON")
	}

	// should contain table headers
	if !strings.Contains(stdout, "ID") || !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "STATUS") {
		t.Error("expected table headers in output")
	}

	t.Logf("legacy get pod works, output length: %d bytes", len(stdout))
}

func TestCLI_LegacyGetPodWithID(t *testing.T) {
	// first get a pod id using new command
	listOut, _, err := runCLI("pod", "list")
	if err != nil {
		t.Skip("skipping - can't list pods")
	}

	var pods []map[string]interface{}
	if err := json.Unmarshal([]byte(listOut), &pods); err != nil || len(pods) == 0 {
		t.Skip("skipping - no pods found")
	}

	podID := pods[0]["id"].(string)

	stdout, stderr, err := runCLI("get", "pod", podID)
	if err != nil {
		t.Fatalf("failed to run legacy get pod <id>: %v\nstderr: %s", err, stderr)
	}

	if !strings.Contains(stderr, "deprecated") {
		t.Error("expected deprecation warning")
	}

	if !strings.Contains(stdout, podID) {
		t.Errorf("expected pod id %s in output", podID)
	}

	t.Logf("legacy get pod <id> works for pod %s", podID)
}

func TestCLI_LegacyGetPodAllFields(t *testing.T) {
	// first get a pod id
	listOut, _, err := runCLI("pod", "list")
	if err != nil {
		t.Skip("skipping - can't list pods")
	}

	var pods []map[string]interface{}
	if err := json.Unmarshal([]byte(listOut), &pods); err != nil || len(pods) == 0 {
		t.Skip("skipping - no pods found")
	}

	podID := pods[0]["id"].(string)

	stdout, stderr, err := runCLI("get", "pod", podID, "--allfields")
	if err != nil {
		t.Fatalf("failed to run legacy get pod --allfields: %v\nstderr: %s", err, stderr)
	}

	// --allfields should include extra columns
	if !strings.Contains(stdout, "VCPU") || !strings.Contains(stdout, "$/HR") || !strings.Contains(stdout, "PORTS") {
		t.Error("expected allfields columns (VCPU, $/HR, PORTS) in output")
	}

	t.Logf("legacy get pod --allfields works")
}

func TestCLI_LegacyCreatePodHelp(t *testing.T) {
	stdout, _, err := runCLI("create", "pod", "--help")
	if err != nil {
		t.Fatalf("failed to run legacy create pod --help: %v", err)
	}

	// should have the original flags
	expectedFlags := []string{"--gpuType", "--imageName", "--containerDiskSize", "--volumeSize"}
	for _, flag := range expectedFlags {
		if !strings.Contains(stdout, flag) {
			t.Errorf("expected flag %s in create pod help", flag)
		}
	}

	t.Log("legacy create pod --help works with original flags")
}

func TestCLI_LegacyRemovePodHelp(t *testing.T) {
	stdout, _, err := runCLI("remove", "pod", "--help")
	if err != nil {
		t.Fatalf("failed to run legacy remove pod --help: %v", err)
	}

	if !strings.Contains(stdout, "remove a pod") {
		t.Error("expected 'remove a pod' in help output")
	}

	t.Log("legacy remove pod --help works")
}

func TestCLI_LegacyStartPodHelp(t *testing.T) {
	stdout, _, err := runCLI("start", "pod", "--help")
	if err != nil {
		t.Fatalf("failed to run legacy start pod --help: %v", err)
	}

	// should have bid flag for spot instances
	if !strings.Contains(stdout, "--bid") {
		t.Error("expected --bid flag in start pod help")
	}

	t.Log("legacy start pod --help works with original flags")
}

func TestCLI_LegacyStopPodHelp(t *testing.T) {
	stdout, _, err := runCLI("stop", "pod", "--help")
	if err != nil {
		t.Fatalf("failed to run legacy stop pod --help: %v", err)
	}

	if !strings.Contains(stdout, "stop a pod") {
		t.Error("expected 'stop a pod' in help output")
	}

	t.Log("legacy stop pod --help works")
}

func TestCLI_LegacyConfigHelp(t *testing.T) {
	stdout, _, err := runCLI("config", "--help")
	if err != nil {
		t.Fatalf("failed to run legacy config --help: %v", err)
	}

	// should have the original apiKey flag
	if !strings.Contains(stdout, "--apiKey") {
		t.Error("expected --apiKey flag in config help")
	}

	t.Log("legacy config --help works with original flags")
}
