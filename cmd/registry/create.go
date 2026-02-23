package registry

import (
	"fmt"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new registry auth",
	Long:  "create a new container registry authentication",
	Args:  cobra.NoArgs,
	RunE:  runCreate,
}

var (
	createName     string
	createUsername string
	createPassword string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "registry auth name (required)")
	createCmd.Flags().StringVar(&createUsername, "username", "", "registry username (required)")
	createCmd.Flags().StringVar(&createPassword, "password", "", "registry password (required)")

	createCmd.MarkFlagRequired("name")     //nolint:errcheck
	createCmd.MarkFlagRequired("username") //nolint:errcheck
	createCmd.MarkFlagRequired("password") //nolint:errcheck
}

func runCreate(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.ContainerRegistryAuthCreateRequest{
		Name:     createName,
		Username: createUsername,
		Password: createPassword,
	}

	auth, err := client.CreateContainerRegistryAuth(req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to create registry auth: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(auth, &output.Config{Format: format})
}
