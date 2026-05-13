package template

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <template-id>",
	Short: "update a template",
	Long:  "update an existing template",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

var (
	updateName              string
	updateImageName         string
	updatePorts             string
	updateEnv               string
	updateReadme            string
	updateContainerDiskInGb int
)

func init() {
	updateCmd.Flags().StringVar(&updateName, "name", "", "new template name")
	updateCmd.Flags().StringVar(&updateImageName, "image", "", "new docker image name")
	updateCmd.Flags().StringVar(&updatePorts, "ports", "", "new comma-separated list of ports")
	updateCmd.Flags().StringVar(&updateEnv, "env", "", "new environment variables as json object")
	updateCmd.Flags().StringVar(&updateReadme, "readme", "", "new readme content")
	updateCmd.Flags().IntVar(&updateContainerDiskInGb, "container-disk-in-gb", -1, "new container disk size in gb")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	templateID := args[0]

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	req := &api.TemplateUpdateRequest{}

	if updateName != "" {
		req.Name = updateName
	}
	if updateImageName != "" {
		req.ImageName = updateImageName
	}
	if updatePorts != "" {
		req.Ports = strings.Split(updatePorts, ",")
	}
	if updateEnv != "" {
		var env map[string]string
		if err := json.Unmarshal([]byte(updateEnv), &env); err != nil {
			return fmt.Errorf("invalid env json: %w", err)
		}
		req.Env = env
	}
	if updateReadme != "" {
		req.Readme = updateReadme
	}
	if updateContainerDiskInGb >= 0 {
		req.ContainerDiskInGb = &updateContainerDiskInGb
	}

	template, err := client.UpdateTemplate(templateID, req)
	if err != nil {
		return fmt.Errorf("failed to update template: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(template, &output.Config{Format: format})
}
