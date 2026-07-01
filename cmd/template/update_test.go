package template

import "testing"

func TestUpdateCmd_HasContainerDiskFlag(t *testing.T) {
	flag := updateCmd.Flags().Lookup("container-disk-in-gb")
	if flag == nil {
		t.Fatal("expected container-disk-in-gb flag")
	}
}

func TestUpdateCmd_HasRegistryAuthFlag(t *testing.T) {
	if updateCmd.Flags().Lookup("registry-auth-id") == nil {
		t.Fatal("expected registry-auth-id flag")
	}
}

func TestCreateCmd_HasRegistryAuthFlag(t *testing.T) {
	if createCmd.Flags().Lookup("registry-auth-id") == nil {
		t.Fatal("expected registry-auth-id flag")
	}
}
