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
