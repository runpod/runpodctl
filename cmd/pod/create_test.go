package pod

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
)

func TestParseDockerArgs(t *testing.T) {
	cases := []struct {
		name           string
		in             string
		wantCmd        []string
		wantEntrypoint []string
	}{
		{
			name:    "simple command",
			in:      "sleep infinity",
			wantCmd: []string{"sleep", "infinity"},
		},
		{
			name:    "single token",
			in:      "nginx",
			wantCmd: []string{"nginx"},
		},
		{
			name:    "quoted argument stays one token",
			in:      `bash -c "sleep infinity"`,
			wantCmd: []string{"bash", "-c", "sleep infinity"},
		},
		{
			name:    "single quotes",
			in:      `sh -c 'while true; do date; sleep 60; done'`,
			wantCmd: []string{"sh", "-c", "while true; do date; sleep 60; done"},
		},
		{
			name:    "extra whitespace",
			in:      "  sleep   infinity  ",
			wantCmd: []string{"sleep", "infinity"},
		},
		{
			name:    "unbalanced quote falls back to whitespace split",
			in:      `sleep "infinity`,
			wantCmd: []string{"sleep", `"infinity`},
		},
		{
			name: "whitespace-only input yields no tokens",
			in:   "   ",
		},
		{
			name:           "canonical json object form",
			in:             `{"cmd":["sleep","infinity"],"entrypoint":["/bin/sh","-c"]}`,
			wantCmd:        []string{"sleep", "infinity"},
			wantEntrypoint: []string{"/bin/sh", "-c"},
		},
		{
			name:    "json object with cmd only",
			in:      `{"cmd":["python -u app.py"]}`,
			wantCmd: []string{"python -u app.py"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, entrypoint := parseDockerArgs(tc.in)
			assertTokens(t, "cmd", cmd, tc.wantCmd)
			assertTokens(t, "entrypoint", entrypoint, tc.wantEntrypoint)
		})
	}
}

func assertTokens(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", field, got, want)
	}
}

// The CON-842 regression was the wire format: the rest POST /pods schema has
// no dockerArgs field and 400s on it, so the request must serialize the start
// command as dockerStartCmd and never as dockerArgs.
func TestPodCreateRequestDockerArgsWireFormat(t *testing.T) {
	cmd, entrypoint := parseDockerArgs("sleep infinity")
	body, err := json.Marshal(&api.PodCreateRequest{
		ImageName:        "ubuntu:22.04",
		ComputeType:      "CPU",
		DockerStartCmd:   cmd,
		DockerEntrypoint: entrypoint,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(body), "dockerArgs") {
		t.Fatalf("request body must not contain dockerArgs: %s", body)
	}
	if !strings.Contains(string(body), `"dockerStartCmd":["sleep","infinity"]`) {
		t.Fatalf("request body missing dockerStartCmd tokens: %s", body)
	}
}
