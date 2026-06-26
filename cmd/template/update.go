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
	Example: `  runpodctl template update <template-id> --registry-auth-id <registry-auth-id>
  runpodctl template update <template-id> --port-labels "22=ssh,8888=jupyter lab"`,
	Args: cobra.ExactArgs(1),
	RunE: runUpdate,
}

var (
	updateName              string
	updateImageName         string
	updatePorts             string
	updatePortLabels        string
	updateEnv               string
	updateReadme            string
	updateContainerDiskInGb int
	updateRegistryAuthID    string
)

func init() {
	updateCmd.Flags().StringVar(&updateName, "name", "", "new template name")
	updateCmd.Flags().StringVar(&updateImageName, "image", "", "new docker image name")
	updateCmd.Flags().StringVar(&updatePorts, "ports", "", "new comma-separated list of ports")
	updateCmd.Flags().StringVar(&updatePortLabels, "port-labels", "", "new port labels as port=name pairs or json; pass an empty value to clear")
	updateCmd.Flags().StringVar(&updateEnv, "env", "", "new environment variables as json object")
	updateCmd.Flags().StringVar(&updateReadme, "readme", "", "new readme content")
	updateCmd.Flags().IntVar(&updateContainerDiskInGb, "container-disk-in-gb", -1, "new container disk size in gb")
	updateCmd.Flags().StringVar(&updateRegistryAuthID, "registry-auth-id", "", "new container registry auth id; pass an empty value to clear")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	templateID := args[0]
	portLabelsChanged := cmd.Flags().Changed("port-labels")

	var portLabels []api.TemplatePortConfig
	if portLabelsChanged {
		var err error
		portLabels, err = parsePortLabels(updatePortLabels)
		if err != nil {
			return err
		}
		if strings.TrimSpace(updatePorts) != "" {
			if err := validatePortLabelsAgainstPorts(portLabels, updatePorts); err != nil {
				return err
			}
		}
	}

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

	registryAuthChanged := cmd.Flags().Changed("registry-auth-id")
	if registryAuthChanged {
		value := strings.TrimSpace(updateRegistryAuthID)
		req.ContainerRegistryAuthID = &value
	}

	hasRESTUpdate := req.Name != "" || req.ImageName != "" || req.Ports != nil || req.Env != nil || req.Readme != "" ||
		req.ContainerDiskInGb != nil || req.ContainerRegistryAuthID != nil

	var template *api.Template
	if portLabelsChanged && !hasRESTUpdate {
		template, err = client.GetTemplate(templateID)
	} else {
		template, err = client.UpdateTemplate(templateID, req)
	}
	if err != nil {
		return fmt.Errorf("failed to update template: %w", err)
	}

	if portLabelsChanged {
		graphqlClient, graphqlErr := api.NewGraphQLClient()
		if graphqlErr == nil {
			graphqlErr = graphqlClient.UpdateTemplatePortLabels(templateID, portLabels, updatePortLabelOverrides(req))
		}
		if graphqlErr != nil {
			if hasRESTUpdate {
				return fmt.Errorf("template fields updated but failed to update port labels: %w", graphqlErr)
			}
			return fmt.Errorf("failed to update port labels: %w", graphqlErr)
		}
		template.PortsConfig = portLabels
	}
	if req.ContainerRegistryAuthID != nil {
		template.ContainerRegistryAuthID = *req.ContainerRegistryAuthID
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(template, &output.Config{Format: format})
}

func updatePortLabelOverrides(req *api.TemplateUpdateRequest) *api.TemplatePortLabelOverrides {
	overrides := &api.TemplatePortLabelOverrides{
		ContainerDiskInGb:       req.ContainerDiskInGb,
		ContainerRegistryAuthID: req.ContainerRegistryAuthID,
	}
	if req.Name != "" {
		overrides.Name = &req.Name
	}
	if req.ImageName != "" {
		overrides.ImageName = &req.ImageName
	}
	if req.Ports != nil {
		overrides.Ports = &req.Ports
	}
	if req.Env != nil {
		overrides.Env = &req.Env
	}
	if req.Readme != "" {
		overrides.Readme = &req.Readme
	}
	return overrides
}
