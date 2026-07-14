package model

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var (
	removeOwner   string
	removeName    string
	removeHash    string
	removeVersion string
)

var (
	getModelForRemove                    = api.GetModel
	removeModelFromRepo                  = api.RemoveModel
	updateModelVersionStatusByIdentifier = api.UpdateModelVersionStatusByIdentifier
)

var removeCmd = &cobra.Command{
	Use:     "remove",
	Aliases: []string{"rm", "delete"},
	Args:    cobra.ExactArgs(0),
	Short:   "remove a model",
	Long:    "remove a model from the runpod model repository",
	Run:     runRemoveModel,
}

var RemoveModelCmd = &cobra.Command{
	Use:    "model",
	Args:   cobra.ExactArgs(0),
	Short:  "deprecated: use 'runpodctl model remove'",
	Long:   "",
	Hidden: true,
	Run:    runRemoveModel,
}

func init() {
	bindRemoveModelFlags(removeCmd)
	bindRemoveModelFlags(RemoveModelCmd)
	removeCmd.MarkFlagRequired("owner")      //nolint
	removeCmd.MarkFlagRequired("name")       //nolint
	RemoveModelCmd.MarkFlagRequired("owner") //nolint
	RemoveModelCmd.MarkFlagRequired("name")  //nolint
}

func bindRemoveModelFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&removeOwner, "owner", "", "model owner")
	cmd.Flags().StringVar(&removeName, "name", "", "model name")
	cmd.Flags().StringVar(&removeHash, "hash", "", "model version hash to remove")
	cmd.Flags().StringVar(&removeVersion, "version", "", "model version uuid to remove")
}

func runRemoveModel(cmd *cobra.Command, args []string) {
	if removeOwner == "" || removeName == "" {
		cobra.CheckErr(fmt.Errorf("both --owner and --name must be provided"))
		return
	}

	hash := strings.TrimSpace(removeHash)
	version := strings.TrimSpace(removeVersion)
	if hash != "" && version != "" {
		cobra.CheckErr(fmt.Errorf("only one of --hash or --version can be provided"))
		return
	}

	var result *api.ModelRepoMutationResult
	var err error
	if hash != "" || version != "" {
		result, err = removeModelVersion(removeOwner, removeName, hash, version)
	} else {
		input := &api.RemoveModelInput{
			Owner: removeOwner,
			Name:  removeName,
		}
		result, err = removeModelFromRepo(input)
	}

	if err != nil {
		if handleModelRepoError(err) {
			return
		}

		cobra.CheckErr(err)
		return
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	cobra.CheckErr(output.Print(result, &output.Config{Format: format}))
}

func removeModelVersion(owner, name, hash, version string) (*api.ModelRepoMutationResult, error) {
	model, err := getModelForRemove(&api.GetModelInput{
		Owner: owner,
		Name:  name,
	})
	if err != nil {
		return nil, err
	}

	target := findModelVersion(model, hash, version)
	if target == nil {
		if hash != "" {
			return nil, fmt.Errorf("model version hash %q not found for %s/%s", hash, owner, name)
		}
		return nil, fmt.Errorf("model version %q not found for %s/%s", version, owner, name)
	}

	input := &api.UpdateModelVersionStatusInput{
		Status: api.ModelVersionStatusPodRemoved,
	}
	if hash != "" {
		input.Hash = target.Hash
	} else {
		input.UUID = target.UUID
	}

	updatedVersion, err := updateModelVersionStatusByIdentifier(input)
	if err != nil {
		return nil, err
	}
	patchModelVersion(model, target, updatedVersion)

	return &api.ModelRepoMutationResult{
		Success: true,
		Model:   model,
		Version: updatedVersion,
	}, nil
}

func findModelVersion(model *api.Model, hash, version string) *api.ModelVersion {
	if model == nil {
		return nil
	}

	for _, modelVersion := range model.Versions {
		if modelVersion == nil {
			continue
		}
		if hash != "" && modelVersion.Hash == hash {
			return modelVersion
		}
		if version != "" && modelVersion.UUID == version {
			return modelVersion
		}
	}

	return nil
}

func patchModelVersion(model *api.Model, target, updated *api.ModelVersion) {
	if model == nil || target == nil || updated == nil {
		return
	}

	for i, modelVersion := range model.Versions {
		if modelVersion == target {
			model.Versions[i] = updated
			return
		}
	}
}
