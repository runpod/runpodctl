package template

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new template",
	Long:  "create a new template",
	Example: `  runpodctl template create --name private-gpu --image registry.example.com/team/image:tag --registry-auth-id <registry-auth-id>
  runpodctl template create --name dev --image example/image:tag --ports "22/tcp,8888/http" --port-labels "22=ssh,8888=jupyter lab"`,
	Args: cobra.NoArgs,
	RunE: runCreate,
}

var (
	createName              string
	createImageName         string
	createIsServerless      bool
	createPorts             string
	createPortLabels        string
	createDockerEntrypoint  string
	createDockerStartCmd    string
	createEnv               string
	createContainerDiskInGb int
	createVolumeInGb        int
	createVolumeMountPath   string
	createReadme            string
	createRegistryAuthID    string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "template name (required)")
	createCmd.Flags().StringVar(&createImageName, "image", "", "docker image name (required)")
	createCmd.Flags().BoolVar(&createIsServerless, "serverless", false, "is this a serverless template")
	createCmd.Flags().StringVar(&createPorts, "ports", "", "comma-separated list of ports")
	createCmd.Flags().StringVar(&createPortLabels, "port-labels", "", "port labels as comma-separated port=name pairs, or json when a name contains a comma (requires --ports)")
	createCmd.Flags().StringVar(&createDockerEntrypoint, "docker-entrypoint", "", "comma-separated docker entrypoint commands")
	createCmd.Flags().StringVar(&createDockerStartCmd, "docker-start-cmd", "", "comma-separated docker start commands")
	createCmd.Flags().StringVar(&createEnv, "env", "", "environment variables as json object")
	createCmd.Flags().IntVar(&createContainerDiskInGb, "container-disk-in-gb", 20, "container disk size in gb")
	createCmd.Flags().IntVar(&createVolumeInGb, "volume-in-gb", 0, "volume size in gb")
	createCmd.Flags().StringVar(&createVolumeMountPath, "volume-mount-path", "/workspace", "volume mount path")
	createCmd.Flags().StringVar(&createReadme, "readme", "", "readme content")
	createCmd.Flags().StringVar(&createRegistryAuthID, "registry-auth-id", "", "container registry auth id (from 'runpodctl registry list')")

	createCmd.MarkFlagRequired("name")  //nolint:errcheck
	createCmd.MarkFlagRequired("image") //nolint:errcheck
}

func runCreate(cmd *cobra.Command, args []string) error {
	var portLabels []api.TemplatePortConfig
	if cmd.Flags().Changed("port-labels") {
		var err error
		portLabels, err = parsePortLabels(createPortLabels)
		if err != nil {
			return err
		}
		if len(portLabels) > 0 && strings.TrimSpace(createPorts) == "" {
			return fmt.Errorf("--port-labels requires --ports when creating a template")
		}
		if err := validatePortLabelsAgainstPorts(portLabels, createPorts); err != nil {
			return err
		}
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	req := &api.TemplateCreateRequest{
		Name:                    createName,
		ImageName:               createImageName,
		IsServerless:            createIsServerless,
		ContainerDiskInGb:       createContainerDiskInGb,
		ContainerRegistryAuthID: strings.TrimSpace(createRegistryAuthID),
		Readme:                  createReadme,
	}

	// serverless templates do not support volume fields
	if !createIsServerless {
		req.VolumeInGb = createVolumeInGb
		req.VolumeMountPath = createVolumeMountPath
	}

	if createPorts != "" {
		req.Ports = strings.Split(createPorts, ",")
	}
	if createDockerEntrypoint != "" {
		req.DockerEntrypoint = strings.Split(createDockerEntrypoint, ",")
	}
	if createDockerStartCmd != "" {
		req.DockerStartCmd = strings.Split(createDockerStartCmd, ",")
	}
	if createEnv != "" {
		var env map[string]string
		if err := json.Unmarshal([]byte(createEnv), &env); err != nil {
			return fmt.Errorf("invalid env json: %w", err)
		}
		req.Env = env
	}

	template, err := client.CreateTemplate(req)
	if err != nil {
		return fmt.Errorf("failed to create template: %w", err)
	}

	// Port labels (portsConfig) are not part of the REST template schema, so
	// they are applied via a follow-up GraphQL saveTemplate. If that fails the
	// template would exist without labels, so delete it to keep create atomic.
	if len(portLabels) > 0 {
		graphqlClient, labelErr := api.NewGraphQLClient()
		if labelErr == nil {
			labelErr = graphqlClient.UpdateTemplatePortLabels(template.ID, portLabels, createPortLabelOverrides(req))
		}
		if labelErr != nil {
			if cleanupErr := client.DeleteTemplate(template.ID); cleanupErr != nil {
				labelErr = fmt.Errorf("failed to set port labels: %v; failed to clean up template %s: %w", labelErr, template.ID, cleanupErr)
			} else {
				labelErr = fmt.Errorf("failed to set port labels: %w", labelErr)
			}
			return labelErr
		}
		template.PortsConfig = portLabels
	}
	if req.ContainerRegistryAuthID != "" {
		template.ContainerRegistryAuthID = req.ContainerRegistryAuthID
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(template, &output.Config{Format: format})
}

func createPortLabelOverrides(req *api.TemplateCreateRequest) *api.TemplatePortLabelOverrides {
	overrides := &api.TemplatePortLabelOverrides{
		Name:              &req.Name,
		ImageName:         &req.ImageName,
		IsServerless:      &req.IsServerless,
		Ports:             &req.Ports,
		Env:               &req.Env,
		ContainerDiskInGb: &req.ContainerDiskInGb,
		Readme:            &req.Readme,
	}
	if req.ContainerRegistryAuthID != "" {
		overrides.ContainerRegistryAuthID = &req.ContainerRegistryAuthID
	}
	if !req.IsServerless {
		overrides.VolumeInGb = &req.VolumeInGb
		overrides.VolumeMountPath = &req.VolumeMountPath
	}
	if dockerArgs := dockerArgsJSON(req.DockerEntrypoint, req.DockerStartCmd); dockerArgs != "" {
		overrides.DockerArgs = &dockerArgs
	}
	return overrides
}

// dockerArgsJSON reconstructs the backend's canonical dockerArgs encoding
// (`{"cmd":[...],"entrypoint":[...]}`) from the REST create fields. The port-label
// write reads the template back over GraphQL and re-sends dockerArgs; if that
// post-create read is briefly stale it returns an empty dockerArgs and the write
// would blank the just-set start command. Carrying the reconstructed value through
// the overrides makes the write re-assert the command regardless of read staleness.
// Returns "" when neither field is set (so the override stays nil and a genuinely
// command-less template is left untouched).
func dockerArgsJSON(entrypoint, cmd []string) string {
	if len(entrypoint) == 0 && len(cmd) == 0 {
		return ""
	}
	payload := struct {
		Cmd        []string `json:"cmd,omitempty"`
		Entrypoint []string `json:"entrypoint,omitempty"`
	}{Cmd: cmd, Entrypoint: entrypoint}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}
