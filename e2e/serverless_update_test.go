//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
)

func TestE2E_ServerlessUpdateV2(t *testing.T) {
	invokeBinary(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	client, err := api.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("con-2158-%d", time.Now().UnixNano())
	data, err := client.PostV2("/serverless", map[string]any{
		"name": name, "image": mockWorkerImage, "type": "QUEUE",
		"gpu":     map[string]any{"pools": []string{"ADA_24"}, "count": 1},
		"workers": map[string]any{"min": 0, "max": 0, "idleTimeout": 5},
		"scaling": map[string]any{"type": "QUEUE_DELAY", "queueDelay": 0.5},
	})
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &created); err != nil || created.ID == "" {
		t.Fatalf("decode created endpoint: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DeleteEndpoint(created.ID); err != nil {
			t.Errorf("clean up endpoint %s: %v", created.ID, err)
		}
	})
	update := func(flags ...string) map[string]any {
		t.Helper()
		args := append([]string{"serverless", "update", created.ID}, flags...)
		result := runInvokeCLI(t, args...)
		if result.exitCode != 0 {
			t.Fatalf("update %v failed: %s", flags, result.stderr)
		}
		return result.stdoutJSON(t)
	}
	got := update("--name", name+"-renamed", "--workers-min", "0", "--workers-max", "0", "--idle-timeout", "9")
	if got["name"] != name+"-renamed" || got["idleTimeout"] != float64(9) || got["workersMin"] != float64(0) || got["workersMax"] != float64(0) || got["scalerValue"] != 0.5 {
		t.Fatalf("unexpected worker update: %v", got)
	}
	got = update("--scale-threshold", "3")
	if got["scalerType"] != "QUEUE_DELAY" || got["scalerValue"] != float64(3) {
		t.Fatalf("unexpected threshold update: %v", got)
	}
	got = update("--scale-by", "requests")
	if got["scalerType"] != "REQUEST_COUNT" || got["scalerValue"] != float64(3) {
		t.Fatalf("unexpected strategy update: %v", got)
	}
	for _, args := range [][]string{{"serverless", "get", created.ID}, {"serverless", "list"}} {
		result := runInvokeCLI(t, args...)
		if result.exitCode != 0 {
			t.Fatalf("%v failed: %s", args, result.stderr)
		}
		var endpoints []map[string]any
		if args[1] == "get" {
			endpoints = append(endpoints, result.stdoutJSON(t))
		} else if err := json.Unmarshal([]byte(result.stdout), &endpoints); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, endpoint := range endpoints {
			if endpoint["id"] == created.ID {
				found = true
				if idle, ok := endpoint["idleTimeout"]; !ok || idle != nil {
					t.Fatalf("expected inapplicable idle timeout: %v", endpoint)
				}
			}
		}
		if !found {
			t.Fatalf("endpoint missing from %v", args)
		}
	}
	result := runInvokeCLI(t, "serverless", "update", created.ID, "--idle-timeout", "10")
	if result.exitCode == 0 || result.stderrError(t).Status != 400 {
		t.Fatalf("expected inapplicable idle timeout rejection: %s", result.stderr)
	}
	got = update("--scale-by", "delay", "--scale-threshold", "2", "--idle-timeout", "7")
	if got["scalerType"] != "QUEUE_DELAY" || got["scalerValue"] != float64(2) || got["idleTimeout"] != float64(7) {
		t.Fatalf("unexpected combined update: %v", got)
	}
}
