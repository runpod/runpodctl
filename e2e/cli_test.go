//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
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

func runCLIWithInput(dir string, input string, args ...string) (string, string, error) {
	// use the binary from go/bin
	home, _ := os.UserHomeDir()
	binary := home + "/go/bin/runpod"

	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	if strings.TrimSpace(input) != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func parseStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func pickCommunityGpuType(t *testing.T) string {
	t.Helper()

	stdout, stderr, err := runCLI("gpu", "list")
	if err != nil {
		t.Skipf("skipping - can't list gpus: %v\nstderr: %s", err, stderr)
	}

	var gpus []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &gpus); err != nil {
		t.Skipf("skipping - can't parse gpu list: %v", err)
	}

	for _, gpu := range gpus {
		community, _ := gpu["communityCloud"].(bool)
		available, _ := gpu["available"].(bool)
		id, _ := gpu["gpuTypeId"].(string)
		if community && available && strings.TrimSpace(id) != "" {
			return id
		}
	}

	t.Skip("skipping - no community gpu types available")
	return ""
}

func shouldSkipCommunityCreate(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "no longer any instances available") ||
		strings.Contains(lower, "no instances available") ||
		strings.Contains(lower, "not supported") ||
		strings.Contains(lower, "not enabled") ||
		strings.Contains(lower, "insufficient") ||
		strings.Contains(lower, "quota") ||
		strings.Contains(lower, "out of capacity")
}

func waitForPodSSHCommand(t *testing.T, podID string, attempts int, delay time.Duration) map[string]interface{} {
	t.Helper()

	for i := 0; i < attempts; i++ {
		stdout, stderr, err := runCLI("pod", "get", podID)
		if err == nil {
			var pod map[string]interface{}
			if err := json.Unmarshal([]byte(stdout), &pod); err == nil {
				sshInfo, ok := pod["ssh"].(map[string]interface{})
				if ok {
					if cmd, ok := sshInfo["ssh_command"].(string); ok && strings.TrimSpace(cmd) != "" {
						return pod
					}
				}
			}
		} else {
			t.Logf("pod get attempt %d failed: %v\nstderr: %s", i+1, err, stderr)
		}

		time.Sleep(delay)
	}

	t.Fatalf("ssh command not available for pod %s after %d attempts", podID, attempts)
	return nil
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

func TestCLI_HelpTextConsistency(t *testing.T) {
	stdout, _, err := runCLI("--help")
	if err != nil {
		t.Fatalf("failed to run --help: %v", err)
	}
	if strings.Contains(stdout, "(s)") {
		t.Error("help output should not contain '(s)'")
	}

	re := regexp.MustCompile(`^\\s{2}([a-z0-9-]+)\\s+(.+)$`)
	for _, line := range strings.Split(stdout, "\n") {
		match := re.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		desc := strings.TrimSpace(match[2])
		if desc == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(desc)
		if unicode.IsLetter(r) && !unicode.IsLower(r) {
			t.Errorf("help description should start lowercase: %q", desc)
		}
	}
}

func TestCLI_ProjectCreateLegacy(t *testing.T) {
	tmpDir := t.TempDir()
	projectName := "e2e-project-" + time.Now().Format("20060102150405")
	input := "11.8.0\n3.10\n"

	stdout, stderr, err := runCLIWithInput(tmpDir, input,
		"project", "create",
		"--name", projectName,
		"--type", "Hello_World",
	)
	if err != nil {
		t.Fatalf("failed to run project create: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	projectDir := filepath.Join(tmpDir, projectName)
	tomlPath := filepath.Join(projectDir, "runpod.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("expected runpod.toml to be created: %v", err)
	}

	handlerPath := filepath.Join(projectDir, "src", "handler.py")
	if _, err := os.Stat(handlerPath); err != nil {
		t.Fatalf("expected handler.py to be created: %v", err)
	}

	_, stderr, err = runCLIWithInput(projectDir, "", "project", "build")
	if err != nil {
		t.Fatalf("failed to run project build: %v\nstderr: %s", err, stderr)
	}

	dockerfilePath := filepath.Join(projectDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		t.Fatalf("expected Dockerfile to be created: %v", err)
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

func TestCLI_PodCreateRequiresTemplateOrImage(t *testing.T) {
	// test that pod create fails without template or image
	_, stderr, err := runCLI("pod", "create", "--gpu-type-id", "NVIDIA GeForce RTX 4090")
	if err == nil {
		t.Fatal("expected error when creating pod without template or image")
	}
	if !strings.Contains(stderr, "either --template or --image is required") {
		t.Errorf("expected error about template or image, got: %s", stderr)
	}
}

func TestCLI_PodCreateGlobalNetworkingRequiresSecureCloud(t *testing.T) {
	_, stderr, err := runCLI("pod", "create",
		"--global-networking",
		"--cloud-type", "community",
		"--data-center-ids", "US-MO-2",
		"--image", "ubuntu:22.04",
		"--gpu-type-id", "NVIDIA GeForce RTX 3090",
	)
	if err == nil {
		t.Fatal("expected error when using --global-networking with community cloud")
	}
	lower := strings.ToLower(stderr)
	if !strings.Contains(lower, "global networking") || !strings.Contains(lower, "secure cloud") {
		t.Errorf("expected global networking secure cloud error, got: %s", stderr)
	}
	if !strings.Contains(lower, "data-center-ids") {
		t.Errorf("expected data-center-ids hint, got: %s", stderr)
	}
}

func TestCLI_PodCreateCommunityPublicIP(t *testing.T) {
	gpuTypeID := pickCommunityGpuType(t)
	name := "e2e-test-public-ip-" + time.Now().Format("20060102150405")

	stdout, stderr, err := runCLI("pod", "create",
		"--cloud-type", "community",
		"--public-ip",
		"--image", "ubuntu:22.04",
		"--gpu-type-id", gpuTypeID,
		"--name", name,
	)
	if err != nil {
		if shouldSkipCommunityCreate(stdout + stderr) {
			t.Skipf("community public ip unavailable: %s", strings.TrimSpace(stderr))
		}
		t.Fatalf("failed to create community pod with public ip: %v\nstderr: %s", err, stderr)
	}

	var pod map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &pod); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	podID, ok := pod["id"].(string)
	if !ok || strings.TrimSpace(podID) == "" {
		t.Fatal("expected pod id in response")
	}

	t.Cleanup(func() {
		_, _, err := runCLI("pod", "delete", podID)
		if err != nil {
			t.Logf("warning: failed to delete test pod %s: %v", podID, err)
		} else {
			t.Logf("cleaned up pod %s", podID)
		}
	})

	stdout, stderr, err = runCLI("pod", "get", podID, "--include-machine")
	if err != nil {
		t.Fatalf("failed to get pod %s: %v\nstderr: %s", podID, err, stderr)
	}

	var details map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &details); err != nil {
		t.Fatalf("pod get output is not valid json: %v\noutput: %s", err, stdout)
	}

	machine, ok := details["machine"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected machine info in pod get response")
	}

	supportPublicIP, ok := machine["supportPublicIp"].(bool)
	if !ok {
		t.Fatalf("expected machine.supportPublicIp to be present")
	}
	if !supportPublicIP {
		t.Errorf("expected supportPublicIp true for community pod with --public-ip")
	}
}

func TestCLI_PodCreateFromTemplate(t *testing.T) {
	// create a pod from template
	stdout, stderr, err := runCLI("pod", "create",
		"--template", "runpod-torch-v21",
		"--gpu-type-id", "NVIDIA GeForce RTX 4090",
		"--name", "e2e-test-template-pod")
	if err != nil {
		t.Fatalf("failed to create pod from template: %v\nstderr: %s", err, stderr)
	}

	var pod map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &pod); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	// verify pod was created with template settings
	podID, ok := pod["id"].(string)
	if !ok || podID == "" {
		t.Fatal("expected pod id in response")
	}

	imageName, _ := pod["imageName"].(string)
	if !strings.Contains(imageName, "pytorch") {
		t.Errorf("expected pytorch image from template, got: %s", imageName)
	}

	t.Logf("created pod %s from template with image %s", podID, imageName)

	t.Cleanup(func() {
		_, _, err := runCLI("pod", "delete", podID)
		if err != nil {
			t.Logf("warning: failed to delete test pod %s: %v", podID, err)
		} else {
			t.Logf("cleaned up pod %s", podID)
		}
	})

	podDetails := waitForPodSSHCommand(t, podID, 12, 10*time.Second)
	if createdAt, ok := podDetails["createdAt"].(string); !ok || strings.TrimSpace(createdAt) == "" {
		t.Errorf("expected createdAt to be set for pod %s", podID)
	}

	stdout, stderr, err = runCLI("ssh", "info", podID)
	if err != nil {
		t.Fatalf("failed to run ssh info for pod %s: %v\nstderr: %s", podID, err, stderr)
	}

	var sshInfo map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &sshInfo); err != nil {
		t.Fatalf("ssh info output is not valid json: %v\noutput: %s", err, stdout)
	}
	if cmd, ok := sshInfo["ssh_command"].(string); !ok || strings.TrimSpace(cmd) == "" {
		t.Fatalf("expected ssh_command in ssh info for pod %s", podID)
	}
}

func TestCLI_PodCreateCPU(t *testing.T) {
	name := "e2e-test-cpu-" + time.Now().Format("20060102150405")
	stdout, stderr, err := runCLI("pod", "create",
		"--compute-type", "cpu",
		"--image", "ubuntu:22.04",
		"--name", name)
	if err != nil {
		lower := strings.ToLower(stdout + stderr)
		if strings.Contains(lower, "not supported") ||
			strings.Contains(lower, "not enabled") ||
			strings.Contains(lower, "compute type") ||
			(strings.Contains(lower, "cpu") && strings.Contains(lower, "not")) {
			t.Skipf("cpu pods not available for this account: %s", strings.TrimSpace(stderr))
		}
		t.Fatalf("failed to create cpu pod: %v\nstderr: %s", err, stderr)
	}

	var pod map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &pod); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	podID, ok := pod["id"].(string)
	if !ok || podID == "" {
		t.Fatal("expected pod id in response")
	}

	t.Cleanup(func() {
		_, _, err := runCLI("pod", "delete", podID)
		if err != nil {
			t.Logf("warning: failed to delete test pod %s: %v", podID, err)
		} else {
			t.Logf("cleaned up pod %s", podID)
		}
	})

	stdout, stderr, err = runCLI("pod", "get", podID)
	if err != nil {
		t.Fatalf("failed to get pod %s: %v\nstderr: %s", podID, err, stderr)
	}

	var podDetails map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &podDetails); err != nil {
		t.Fatalf("pod get output is not valid json: %v\noutput: %s", err, stdout)
	}
	if createdAt, ok := podDetails["createdAt"].(string); !ok || strings.TrimSpace(createdAt) == "" {
		t.Errorf("expected createdAt to be set for pod %s", podID)
	}
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

func TestCLI_TemplateSearchOpenclawStack(t *testing.T) {
	// test search for specific template: openclaw-stack
	stdout, stderr, err := runCLI("template", "search", "openclaw-stack")
	if err != nil {
		t.Fatalf("failed to search for openclaw-stack: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if len(templates) == 0 {
		t.Fatal("expected to find openclaw-stack template")
	}

	// verify we found the right template
	found := false
	for _, tpl := range templates {
		name, _ := tpl["name"].(string)
		if name == "openclaw-stack" {
			found = true
			id, _ := tpl["id"].(string)
			isRunpod, _ := tpl["isRunpod"].(bool)
			t.Logf("found openclaw-stack template: id=%s, isRunpod=%v", id, isRunpod)

			// verify it's an official RunPod template
			if !isRunpod {
				t.Error("expected openclaw-stack to be an official RunPod template")
			}
			break
		}
	}

	if !found {
		t.Errorf("openclaw-stack not found in results: %v", templates)
	}
}

func TestCLI_TemplateSearchWithTypeOfficial(t *testing.T) {
	// test search with --type official filter
	stdout, stderr, err := runCLI("template", "search", "pytorch", "--type", "official")
	if err != nil {
		t.Fatalf("failed to search official templates: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if len(templates) == 0 {
		t.Fatal("expected to find official pytorch templates")
	}

	// verify all results are official (isRunpod: true)
	for _, tpl := range templates {
		isRunpod, _ := tpl["isRunpod"].(bool)
		if !isRunpod {
			t.Errorf("expected official template, got community: %v", tpl["name"])
		}
	}

	t.Logf("found %d official pytorch templates", len(templates))
}

func TestCLI_TemplateSearchWithTypeCommunity(t *testing.T) {
	// test search with --type community filter
	stdout, stderr, err := runCLI("template", "search", "comfyui", "--type", "community", "--limit", "5")
	if err != nil {
		t.Fatalf("failed to search community templates: %v\nstderr: %s", err, stderr)
	}

	var templates []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &templates); err != nil {
		t.Fatalf("output is not valid json: %v\noutput: %s", err, stdout)
	}

	if len(templates) == 0 {
		t.Fatal("expected to find community comfyui templates")
	}

	// verify all results are community (isRunpod: false or null)
	for _, tpl := range templates {
		isRunpod, _ := tpl["isRunpod"].(bool)
		if isRunpod {
			t.Errorf("expected community template, got official: %v", tpl["name"])
		}
	}

	t.Logf("found %d community comfyui templates", len(templates))
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

	if createdAt, ok := pod["createdAt"].(string); !ok || strings.TrimSpace(createdAt) == "" {
		t.Errorf("expected createdAt to be set")
	}

	sshInfo, ok := pod["ssh"].(map[string]interface{})
	if !ok {
		t.Errorf("expected ssh info to be present")
	} else if cmd, ok := sshInfo["ssh_command"].(string); !ok || strings.TrimSpace(cmd) == "" {
		if _, hasError := sshInfo["error"]; !hasError {
			t.Errorf("expected ssh_command or error in ssh info")
		}
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
	templateID := "runpod-torch-v21"
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

	readme, ok := template["readme"].(string)
	if !ok || strings.TrimSpace(readme) == "" {
		t.Errorf("expected template readme to be present")
	}

	ports := parseStringSlice(template["ports"])
	if len(ports) == 0 {
		t.Errorf("expected template ports to be present")
	}

	t.Logf("got template: %v", template["name"])
}

func TestCLI_ModelList(t *testing.T) {
	stdout, stderr, err := runCLI("model", "list")
	if err != nil {
		t.Fatalf("failed to run model list: %v\nstderr: %s", err, stderr)
	}

	if strings.Contains(stdout, "model repository functionality not yet implemented") ||
		strings.Contains(stderr, "model repository functionality not yet implemented") ||
		strings.Contains(stdout, "Model Repo feature is not enabled for this user") ||
		strings.Contains(stderr, "Model Repo feature is not enabled for this user") {
		t.Skip("model repository not enabled for this account")
	}

	if strings.TrimSpace(stdout) == "" {
		t.Error("expected model list output")
	}
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
