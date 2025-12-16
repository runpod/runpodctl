package model

import (
	"errors"
	"fmt"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var (
	removeOwner string
	removeName  string
)

// RemoveModelCmd removes a model from the RunPod model repository.
// Hidden while the model repository feature is in development and not ready for general use.
var RemoveModelCmd = &cobra.Command{
	Use:    "model",
	Args:   cobra.ExactArgs(0),
	Short:  "remove a model",
	Long:   "remove a model from the RunPod model repository",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		if removeOwner == "" || removeName == "" {
			cobra.CheckErr(fmt.Errorf("both --owner and --name must be provided"))
			return
		}

		input := &api.RemoveModelInput{
			Owner: removeOwner,
			Name:  removeName,
		}

		result, err := api.RemoveModel(input)
		if err != nil {
			if errors.Is(err, api.ErrModelRepoNotImplemented) {
				fmt.Println(api.ErrModelRepoNotImplemented.Error())
				return
			}

			cobra.CheckErr(err)
			return
		}

		fmt.Println("model removal requested")

		if result != nil && result.Model != nil && len(result.Model.Versions) > 0 {
			fmt.Println("affected versions:")
			for _, version := range result.Model.Versions {
				if version == nil {
					continue
				}
				hash := version.Hash
				if hash == "" {
					hash = version.VersionHash
				}
				fmt.Printf("- %s (%s)\n", hash, version.Status)
			}
		}
	},
}

func init() {
	RemoveModelCmd.Flags().StringVar(&removeOwner, "owner", "", "account or namespace that owns the model")
	RemoveModelCmd.Flags().StringVar(&removeName, "name", "", "model name within the owner namespace")

	RemoveModelCmd.MarkFlagRequired("owner") //nolint
	RemoveModelCmd.MarkFlagRequired("name")  //nolint
}
