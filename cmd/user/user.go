package user

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

// Cmd is the user command
var Cmd = &cobra.Command{
	Use:     "user",
	Aliases: []string{"account", "me"},
	Short:   "show account info",
	Long:    "show current user account info including balance and spend",
	Args:    cobra.NoArgs,
	RunE:    runUser,
}

func runUser(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	user, err := client.GetUser()
	if err != nil {
		output.Error(err)
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(user, &output.Config{Format: format})
}
