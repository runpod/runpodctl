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

var RemoveModelCmd = &cobra.Command{
	Use:    "model",
	Args:   cobra.ExactArgs(0),
	Short:  "internal command",
	Long:   "",
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
	RemoveModelCmd.Flags().StringVar(&removeOwner, "owner", "", "")
	RemoveModelCmd.Flags().StringVar(&removeName, "name", "", "")

	RemoveModelCmd.MarkFlagRequired("owner") //nolint
	RemoveModelCmd.MarkFlagRequired("name")  //nolint
}
