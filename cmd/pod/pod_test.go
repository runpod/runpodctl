package pod

import (
	"bytes"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestPodCmd_Structure(t *testing.T) {
	if Cmd.Use != "pod" {
		t.Errorf("expected use 'pod', got %s", Cmd.Use)
	}

	// check aliases
	found := false
	for _, alias := range Cmd.Aliases {
		if alias == "pods" {
			found = true
		}
	}
	if !found {
		t.Error("expected alias 'pods'")
	}

	// check subcommands exist
	expectedSubcommands := []string{"list", "get <pod-id>", "create", "update <pod-id>", "start <pod-id>", "stop <pod-id>", "restart <pod-id>", "reset <pod-id>", "delete <pod-id>"}
	for _, expected := range expectedSubcommands {
		found := false
		for _, cmd := range Cmd.Commands() {
			if cmd.Use == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %s not found", expected)
		}
	}
}

func TestListCmd_Flags(t *testing.T) {
	flags := listCmd.Flags()

	if flags.Lookup("compute-type") == nil {
		t.Error("expected --compute-type flag")
	}
	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("status") == nil {
		t.Error("expected --status flag")
	}
	if flags.Lookup("since") == nil {
		t.Error("expected --since flag")
	}
	if flags.Lookup("created-after") == nil {
		t.Error("expected --created-after flag")
	}
	if flags.Lookup("all") == nil {
		t.Error("expected --all flag")
	}
	allFlag := flags.ShorthandLookup("a")
	if allFlag == nil {
		t.Error("expected -a shorthand for --all")
	}
}

func TestCreateCmd_Flags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("image") == nil {
		t.Error("expected --image flag")
	}
	if flags.Lookup("compute-type") == nil {
		t.Error("expected --compute-type flag")
	}
	if flags.Lookup("gpu-id") == nil {
		t.Error("expected --gpu-id flag")
	}
	if flags.Lookup("gpu-count") == nil {
		t.Error("expected --gpu-count flag")
	}
	if flags.Lookup("volume-in-gb") == nil {
		t.Error("expected --volume-in-gb flag")
	}
	if flags.Lookup("global-networking") == nil {
		t.Error("expected --global-networking flag")
	}
	if flags.Lookup("public-ip") == nil {
		t.Error("expected --public-ip flag")
	}
	if flags.Lookup("ssh") == nil {
		t.Error("expected --ssh flag")
	}
	sshFlag := flags.Lookup("ssh")
	if sshFlag.DefValue != "true" {
		t.Errorf("expected --ssh default to be true, got %s", sshFlag.DefValue)
	}
}

func TestDeleteCmd_Aliases(t *testing.T) {
	aliases := deleteCmd.Aliases
	hasRm := false
	hasRemove := false
	for _, alias := range aliases {
		if alias == "rm" {
			hasRm = true
		}
		if alias == "remove" {
			hasRemove = true
		}
	}
	if !hasRm {
		t.Error("expected alias 'rm'")
	}
	if !hasRemove {
		t.Error("expected alias 'remove'")
	}
}

func executeCommand(root *cobra.Command, args ...string) (output string, err error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err = root.Execute()
	return buf.String(), err
}

func TestPodCmd_Help(t *testing.T) {
	output, err := executeCommand(Cmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Error("expected help output")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		// valid inputs
		{input: "1h", want: time.Hour},
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "30m", want: 30 * time.Minute},
		{input: "1h30m", want: time.Hour + 30*time.Minute},
		{input: "2h", want: 2 * time.Hour},
		{input: "1d", want: 24 * time.Hour},

		// invalid inputs
		{input: "-1d", wantErr: true},
		{input: "0h", wantErr: true},
		{input: "0d", wantErr: true},
		{input: "abc", wantErr: true},
		{input: "", wantErr: true},
		{input: "-2h", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDuration(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDuration(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCreatedAt(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		wantZero bool
		wantTime time.Time
	}{
		{
			name:     "RFC3339 string",
			input:    "2025-06-15T10:30:00Z",
			wantZero: false,
			wantTime: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:     "Unix timestamp string",
			input:    "1750000000",
			wantZero: false,
			wantTime: time.Unix(1750000000, 0),
		},
		{
			name:     "invalid string",
			input:    "not-a-date",
			wantZero: true,
		},
		{
			name:     "nil value",
			input:    nil,
			wantZero: true,
		},
		{
			name:     "non-string type (int)",
			input:    12345,
			wantZero: true,
		},
		{
			name:     "empty string",
			input:    "",
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCreatedAt(tt.input)
			if tt.wantZero {
				if !got.IsZero() {
					t.Errorf("parseCreatedAt(%v) = %v, want zero time", tt.input, got)
				}
				return
			}
			if got.IsZero() {
				t.Errorf("parseCreatedAt(%v) returned zero time, want %v", tt.input, tt.wantTime)
				return
			}
			if !got.Equal(tt.wantTime) {
				t.Errorf("parseCreatedAt(%v) = %v, want %v", tt.input, got, tt.wantTime)
			}
		})
	}
}
