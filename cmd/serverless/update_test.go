package serverless

import "testing"

func TestUpdateCmd_HasTemplateIDFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("template-id")
	if flag == nil {
		t.Fatal("expected template-id flag")
	}
}
