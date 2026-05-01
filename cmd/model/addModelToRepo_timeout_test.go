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

func TestUploadModelFilesUsesModelVersionUUIDAfterFirstFile(t *testing.T) {
	oldCreateModelRepoUpload := createModelRepoUpload
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUpload := completeModelRepoUpload
	t.Cleanup(func() {
		createModelRepoUpload = oldCreateModelRepoUpload
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUpload = oldCompleteModelRepoUpload
	})

	files := []modelFile{
		{AbsolutePath: "/tmp/a.bin", RelativePath: "a.bin", Size: 1},
		{AbsolutePath: "/tmp/b.bin", RelativePath: "b.bin", Size: 2},
		{AbsolutePath: "/tmp/c.bin", RelativePath: "c.bin", Size: 3},
	}

	var calls []api.CreateModelRepoUploadInput
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		calls = append(calls, *input)
		return &api.ModelRepoMutationResult{
			Success: true,
			Version: &api.ModelVersion{
				UUID: "version-uuid",
				Hash: "version-hash",
			},
			Upload: &api.ModelRepoUpload{
				SessionID: "session-" + input.FileName,
				Key:       "key-" + input.FileName,
			},
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string) error {
		return nil
	}
	completeModelRepoUpload = func(sessionID string) (*api.CompleteModelRepoUploadResult, error) {
		return &api.CompleteModelRepoUploadResult{
			SessionID: sessionID,
			Status:    "completed",
		}, nil
	}

	err := uploadModelFiles(files, &api.CreateModelRepoUploadInput{Name: "test-model"})
	if err != nil {
		t.Fatalf("uploadModelFiles returned error: %v", err)
	}

	if len(calls) != len(files) {
		t.Fatalf("expected %d createModelRepoUpload calls, got %d", len(files), len(calls))
	}
	if calls[0].ModelVersionUUID != "" {
		t.Fatalf("expected first upload call to omit modelVersionUuid, got %q", calls[0].ModelVersionUUID)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i].ModelVersionUUID != "version-uuid" {
			t.Fatalf("expected call %d to use modelVersionUuid %q, got %q", i, "version-uuid", calls[i].ModelVersionUUID)
		}
	}
}
