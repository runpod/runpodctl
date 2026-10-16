package transfer

import (
	"strings"
	"testing"
)

// TestSendReceiveHelpShowsPairedWorkflow pins that the send/receive help teaches
// the two-step code workflow, which was previously undiscoverable: the Long
// descriptions were one sentence with no examples, and the printed one-time code
// and --code flag appeared nowhere in help.
func TestSendReceiveHelpShowsPairedWorkflow(t *testing.T) {
	if !strings.Contains(SendCmd.Example, "runpodctl receive") {
		t.Errorf("send help should show the paired receive step; got example:\n%s", SendCmd.Example)
	}
	if !strings.Contains(ReceiveCmd.Example, "runpodctl send") {
		t.Errorf("receive help should reference the send step that prints the code; got example:\n%s", ReceiveCmd.Example)
	}
	if !strings.Contains(ReceiveCmd.Example, "--code") {
		t.Errorf("receive help should mention the --code flag; got example:\n%s", ReceiveCmd.Example)
	}
}

// TestHelpExamplesUseRealisticCodes pins that example codes end in a numeric
// relay index, the shape runReceive requires.
func TestHelpExamplesUseRealisticCodes(t *testing.T) {
	for _, cmd := range []string{SendCmd.Example, ReceiveCmd.Example} {
		if strings.Contains(cmd, "1234-word-word-word") {
			t.Errorf("example uses a code without a relay index, receive rejects it:\n%s", cmd)
		}
		if !strings.Contains(cmd, "5254-respect-amen-emotion-14") {
			t.Errorf("example should use a code ending in a relay index:\n%s", cmd)
		}
	}
}

func TestValidateSendCode(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{"empty uses random code", "", false},
		{"too short", "abc", true},
		{"five chars", "abcde", true},
		{"six chars", "abcdef", false},
		{"typical", "my-secret", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateSendCode(tt.code); (err != nil) != tt.wantErr {
				t.Errorf("validateSendCode(%q) err = %v, wantErr %v", tt.code, err, tt.wantErr)
			}
		})
	}
}
