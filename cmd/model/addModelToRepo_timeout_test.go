package model

import (
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestSetModelGraphQLTimeoutWithoutInheritedFlag(t *testing.T) {
	viper.Reset()

	cmd := &cobra.Command{Use: "add"}
	setModelGraphQLTimeout(cmd)

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != modelGraphQLTimeoutValue {
		t.Fatalf("expected graphql timeout %s, got %s", modelGraphQLTimeoutValue, got)
	}
}

func TestSetModelGraphQLTimeoutRespectsExistingConfiguredValue(t *testing.T) {
	viper.Reset()

	existing := 2 * time.Minute
	viper.Set(api.GraphQLTimeoutKey, existing)

	cmd := &cobra.Command{Use: "add"}
	setModelGraphQLTimeout(cmd)

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != existing {
		t.Fatalf("expected graphql timeout to remain %s, got %s", existing, got)
	}
}

func TestSetModelGraphQLTimeoutSetsInheritedFlagWhenUnchanged(t *testing.T) {
	viper.Reset()

	root := &cobra.Command{Use: "runpodctl"}
	root.PersistentFlags().Duration(graphqlTimeoutFlagName, 10*time.Second, "graphql timeout")
	cmd := &cobra.Command{Use: "add"}
	root.AddCommand(cmd)

	setModelGraphQLTimeout(cmd)

	flag := cmd.InheritedFlags().Lookup(graphqlTimeoutFlagName)
	if flag == nil {
		t.Fatal("expected inherited graphql-timeout flag")
	}
	if got := flag.Value.String(); got != modelGraphQLTimeoutValue.String() {
		t.Fatalf("expected inherited flag value %s, got %s", modelGraphQLTimeoutValue, got)
	}
	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != modelGraphQLTimeoutValue {
		t.Fatalf("expected graphql timeout %s, got %s", modelGraphQLTimeoutValue, got)
	}
}

func TestSetModelGraphQLTimeoutSkipsWhenInheritedFlagChanged(t *testing.T) {
	viper.Reset()

	root := &cobra.Command{Use: "runpodctl"}
	root.PersistentFlags().Duration(graphqlTimeoutFlagName, 10*time.Second, "graphql timeout")
	if err := root.PersistentFlags().Set(graphqlTimeoutFlagName, "45s"); err != nil {
		t.Fatalf("failed to set inherited flag: %v", err)
	}

	cmd := &cobra.Command{Use: "add"}
	root.AddCommand(cmd)

	setModelGraphQLTimeout(cmd)

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != 0 {
		t.Fatalf("expected graphql timeout to remain unset, got %s", got)
	}
}
