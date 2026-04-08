package template

import "testing"

func TestUpdateCmd_HasContainerDiskFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("container-disk-in-gb")
	if flag == nil {
		t.Fatal("expected container-disk-in-gb flag")
	}
}
