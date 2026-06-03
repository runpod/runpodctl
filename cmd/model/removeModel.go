package model

import (
	"fmt"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var (
	removeOwner string
	removeName  string
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
}

func runRemoveModel(cmd *cobra.Command, args []string) {
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
		if handleModelRepoError(err) {
			return
		}

		cobra.CheckErr(err)
		return
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	cobra.CheckErr(output.Print(result, &output.Config{Format: format}))
}
