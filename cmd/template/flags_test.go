package template

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestTemplateCreateAndUpdateHaveRegistryAndPortLabelFlags(t *testing.T) {
	commands := map[string]*cobra.Command{
		"create": createCmd,
		"update": updateCmd,
	}

	for commandName, command := range commands {
		for _, flagName := range []string{"registry-auth-id", "port-labels"} {
			if command.Flags().Lookup(flagName) == nil {
				t.Errorf("expected template %s to have --%s", commandName, flagName)
			}
		}
	}
}
