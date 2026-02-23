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
	Args:  cobra.NoArgs,
	RunE:  runCreate,
}

var (
	createName              string
	createImageName         string
	createIsServerless      bool
	createPorts             string
	createDockerEntrypoint  string
	createDockerStartCmd    string
	createEnv               string
	createContainerDiskInGb int
	createVolumeInGb        int
	createVolumeMountPath   string
	createReadme            string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "template name (required)")
	createCmd.Flags().StringVar(&createImageName, "image", "", "docker image name (required)")
	createCmd.Flags().BoolVar(&createIsServerless, "serverless", false, "is this a serverless template")
	createCmd.Flags().StringVar(&createPorts, "ports", "", "comma-separated list of ports")
	createCmd.Flags().StringVar(&createDockerEntrypoint, "docker-entrypoint", "", "comma-separated docker entrypoint commands")
	createCmd.Flags().StringVar(&createDockerStartCmd, "docker-start-cmd", "", "comma-separated docker start commands")
	createCmd.Flags().StringVar(&createEnv, "env", "", "environment variables as json object")
	createCmd.Flags().IntVar(&createContainerDiskInGb, "container-disk-in-gb", 20, "container disk size in gb")
	createCmd.Flags().IntVar(&createVolumeInGb, "volume-in-gb", 0, "volume size in gb")
	createCmd.Flags().StringVar(&createVolumeMountPath, "volume-mount-path", "/workspace", "volume mount path")
	createCmd.Flags().StringVar(&createReadme, "readme", "", "readme content")

	createCmd.MarkFlagRequired("name")  //nolint:errcheck
	createCmd.MarkFlagRequired("image") //nolint:errcheck
}

func runCreate(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.TemplateCreateRequest{
		Name:              createName,
		ImageName:         createImageName,
		IsServerless:      createIsServerless,
		ContainerDiskInGb: createContainerDiskInGb,
		VolumeInGb:        createVolumeInGb,
		VolumeMountPath:   createVolumeMountPath,
		Readme:            createReadme,
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
		output.Error(err)
		return fmt.Errorf("failed to create template: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(template, &output.Config{Format: format})
}
