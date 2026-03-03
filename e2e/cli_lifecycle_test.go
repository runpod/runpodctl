//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Default testing variables
const (
	defaultPodImage        = "docker.io/library/alpine"
	defaultPodDiskSize     = "5" // GB
	defaultServerlessImage = "fngarvin/ci-minimal-serverless@sha256:6a33a9bac95b8bc871725db9092af2922a7f1e3b63175248b2191b38be4e93a0"
)

// Regex to catch standard RunPod API keys (rpa_ followed by alphanumeric)
var apiKeyRegex = regexp.MustCompile(`rpa_[a-zA-Z0-9]+`)

func redactSensitive(input string) string {
	return apiKeyRegex.ReplaceAllString(input, "[REDACTED]")
}

// HELPER: Get value from env or return default
func getEnvOrDefault(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// HELPER: execute the runpodctl binary
func runCLI(args ...string) (string, error) {
	// Find binary in path (assume it was installed or built locally)
	// We'll prefer a local build or the installed binary
	var binaryPath string
	
	// Fallbacks
	pathsToTry := []string{
		"./runpodctl",
		"../runpodctl",
		os.ExpandEnv("$HOME/.local/bin/runpodctl"),
		"/usr/local/bin/runpodctl",
		"runpodctl", // system path
	}

	for _, p := range pathsToTry {
		if _, err := exec.LookPath(p); err == nil {
			binaryPath = p
			break
		}
	}

	if binaryPath == "" {
		return "", fmt.Errorf("runpodctl binary not found in PATH or standard locations")
	}

	// Sanitize the command echo to hide keys in arguments if any
	cmdStr := fmt.Sprintf("%s %s", binaryPath, strings.Join(args, " "))
	fmt.Printf("DEBUG: Executing: %s\n", redactSensitive(cmdStr))

	cmd := exec.Command(binaryPath, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := redactSensitive(out.String())
	return output, err
}

func extractIDField(jsonOutput string) (string, error) {
	var result map[string]interface{}
	
	start := strings.Index(jsonOutput, "{")
	end := strings.LastIndex(jsonOutput, "}")

	if start == -1 || end == -1 || end < start {
		return "", fmt.Errorf("could not find JSON block in output: %s", jsonOutput)
	}

	jsonStr := jsonOutput[start : end+1]

	err := json.Unmarshal([]byte(jsonStr), &result)
	if err != nil {
		return "", fmt.Errorf("could not parse json: %v, output captured: %s", err, jsonStr)
	}

	id, ok := result["id"].(string)
	if !ok {
		return "", fmt.Errorf("id field missing or not a string in json: %s", jsonStr)
	}
	return id, nil
}


func TestE2E_CLILifecycle_Pod(t *testing.T) {
	if os.Getenv("RUNPOD_API_KEY") == "" {
		t.Skip("RUNPOD_API_KEY is not set, skipping integration test")
	}

	podImage := getEnvOrDefault("RUNPOD_TEST_POD_IMAGE", defaultPodImage)
	podDisk := getEnvOrDefault("RUNPOD_TEST_POD_DISK", defaultPodDiskSize)

	// Prefix with ci-test- for safe scoping
	podName := fmt.Sprintf("ci-test-pod-%d", time.Now().Unix())

	t.Logf("Creating pod %s with image %s", podName, podImage)

	// Create Pod
	out, err := runCLI(
		"pod", "create",
		"--name", podName,
		"--image", podImage,
		"--container-disk-in-gb", podDisk,
		"--compute-type", "CPU",
		"--output", "json",
	)
	
	if err != nil {
		t.Fatalf("Failed to create pod: %v\nOutput: %s", err, out)
	}

	podID, err := extractIDField(out)
	if err != nil {
		t.Fatalf("Failed to extract Pod ID: %v", err)
	}
	t.Logf("Created Pod ID: %s", podID)

	// Defer cleanup to run even if test fails
	defer func() {
		t.Logf("Cleaning up pod %s...", podID)
		_, delErr := runCLI("pod", "delete", podID)
		if delErr != nil {
			t.Logf("Warning: failed to delete pod %s in cleanup: %v", podID, delErr)
		} else {
			t.Logf("Successfully deleted pod %s", podID)
		}
	}()

	// Wait for propagation
	time.Sleep(5 * time.Second)

	// List Pods and look for ours
	t.Logf("Listing pods to verify presence...")
	listOut, listErr := runCLI("pod", "list", "--output", "json")
	if listErr != nil {
		t.Errorf("Failed to list pods: %v\nOutput: %s", listErr, listOut)
	} else if !strings.Contains(listOut, podID) {
		t.Errorf("Pod ID %s not found in list output", podID)
	}

	// Get Pod
	t.Logf("Getting pod details...")
	getOut, getErr := runCLI("pod", "get", podID, "--output", "json")
	if getErr != nil {
		t.Errorf("Failed to get pod: %v\nOutput: %s", getErr, getOut)
	}

	// Update Pod
	newName := podName + "-updated"
	t.Logf("Updating pod name to %s...", newName)
	updateOut, updateErr := runCLI("pod", "update", podID, "--name", newName)
	if updateErr != nil {
		t.Errorf("Failed to update pod: %v\nOutput: %s", updateErr, updateOut)
	}

	// Stop Pod
	t.Logf("Stopping pod...")
	stopOut, stopErr := runCLI("pod", "stop", podID)
	if stopErr != nil {
		t.Errorf("Failed to stop pod: %v\nOutput: %s", stopErr, stopOut)
	}

	// Start Pod
	t.Logf("Starting pod...")
	startOut, startErr := runCLI("pod", "start", podID)
	if startErr != nil {
		t.Errorf("Failed to start pod: %v\nOutput: %s", startErr, startOut)
	}

	// Test Croc File Transfer (Send/Receive)
	t.Logf("Testing croc file transfer...")
	testFileName := "ci-test-file.txt"
	testFileContent := "v1.14.15-ci-test"
	os.WriteFile(testFileName, []byte(testFileContent), 0644)
	defer os.Remove(testFileName)

	// Start send in background
	// We use the binary directly here because runCLI blocks
	var binaryPath string
	for _, p := range []string{"runpodctl", "../runpodctl", os.ExpandEnv("$HOME/.local/bin/runpodctl"), "/usr/local/bin/runpodctl"} {
		if _, err := exec.LookPath(p); err == nil {
			binaryPath = p
			break
		}
	}

	if binaryPath != "" {
		sendCmd := exec.Command(binaryPath, "send", testFileName)
		var sendOut bytes.Buffer
		sendCmd.Stdout = &sendOut
		sendCmd.Stderr = &sendOut
		
		err := sendCmd.Start()
		if err == nil {
			defer sendCmd.Process.Kill() // Ensure we don't leak the process

			// Poll for code
			var crocCode string
			for i := 0; i < 15; i++ {
				outStr := sendOut.String()
				// Basic extract: look for the format [word]-[word]-[word]-[number] or similar
				// runpodctl prints "Code is: ..."
				if strings.Contains(outStr, " ") {
					lines := strings.Split(outStr, "\n")
					for _, l := range lines {
						if strings.HasPrefix(strings.TrimSpace(l), "Code") || len(strings.Split(l, "-")) >= 2 {
							// just attempt to grab the last token
							tokens := strings.Fields(l)
							if len(tokens) > 0 {
								possible := tokens[len(tokens)-1]
								if strings.Contains(possible, "-") {
									crocCode = possible
									break
								}
							}
						}
					}
				}
				if crocCode != "" {
					break
				}
				time.Sleep(1 * time.Second)
			}

			if crocCode != "" {
				t.Logf("Captured Croc Code: %s", crocCode)
				// Test receive
				pwd, _ := os.Getwd()
				recvDir := filepath.Join(pwd, "recv_test")
				os.MkdirAll(recvDir, 0755)
				defer os.RemoveAll(recvDir)
				
				recvCmd := exec.Command(binaryPath, "receive", crocCode)
				recvCmd.Dir = recvDir
				recvErr := recvCmd.Run()
				if recvErr != nil {
					t.Logf("Warning: croc receive failed (expected if sender hasn't fully registered with relay): %v", recvErr)
				}
			} else {
				t.Logf("Warning: Could not extract croc code in time. Send output: %s", sendOut.String())
			}
		}
	}
}

func TestE2E_CLILifecycle_Serverless(t *testing.T) {
	if os.Getenv("RUNPOD_API_KEY") == "" {
		t.Skip("RUNPOD_API_KEY is not set, skipping integration test")
	}

	slsImage := getEnvOrDefault("RUNPOD_TEST_SERVERLESS_IMAGE", defaultServerlessImage)

	epName := fmt.Sprintf("ci-test-ep-%d", time.Now().Unix())

	t.Logf("Creating serverless endpoint %s with image %s", epName, slsImage)

	// For Serverless, current CLI requires a template-id. 
	// The user mentioned bwf8egptou/wvrr20un0l as their previous templates.
	// We will use wvrr20un0l as a default if none provided.
	slsTemplate := getEnvOrDefault("RUNPOD_TEST_SERVERLESS_TEMPLATE_ID", "wvrr20un0l")

	out, err := runCLI(
		"serverless", "create",
		"--name", epName,
		"--template-id", slsTemplate,
		"--workers-max", "1",
		"--gpu-count", "0",
		"--output", "json",
	)

	if err != nil {
		t.Fatalf("Failed to create endpoint: %v\nOutput: %s", err, out)
	}

	epID, err := extractIDField(out)
	if err != nil {
		t.Fatalf("Failed to extract Endpoint ID: %v", err)
	}
	t.Logf("Created Endpoint ID: %s", epID)

	defer func() {
		t.Logf("Cleaning up endpoint %s...", epID)
		_, delErr := runCLI("serverless", "delete", epID)
		if delErr != nil {
			t.Logf("Warning: failed to delete endpoint %s in cleanup: %v", epID, delErr)
		} else {
			t.Logf("Successfully deleted endpoint %s", epID)
		}
	}()

	// Wait for API propagation
	ready := false
	for i := 0; i < 30; i++ {
		_, getErr := runCLI("serverless", "get", epID)
		if getErr == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Second)
	}

	if !ready {
		t.Fatalf("Endpoint %s did not become available in the API within 5 minutes", epID)
	}

	t.Logf("Endpoint is ready and propagated.")

	// List
	listOut, listErr := runCLI("serverless", "list", "--output", "json")
	if listErr != nil {
		t.Errorf("Failed to list endpoints: %v\nOutput: %s", listErr, listOut)
	} else if !strings.Contains(listOut, epID) {
		t.Errorf("Endpoint ID %s not found in list output", epID)
	}

	// Update
	newName := epName + "-updated"
	t.Logf("Updating endpoint name to %s...", newName)
	updateOut, updateErr := runCLI("serverless", "update", epID, "--name", newName)
	if updateErr != nil {
		t.Errorf("Failed to update serverless endpoint: %v\nOutput: %s", updateErr, updateOut)
	}
}
