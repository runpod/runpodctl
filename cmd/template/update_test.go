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

// a readme-only write must not be reported as a port label failure
func TestGraphqlWriteName(t *testing.T) {
	cases := map[[2]bool]string{
		{true, false}: "port labels",
		{false, true}: "readme",
		{true, true}:  "port labels and readme",
	}
	for in, want := range cases {
		if got := graphqlWriteName(in[0], in[1]); got != want {
			t.Errorf("graphqlWriteName(%v, %v) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
