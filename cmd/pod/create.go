package pod

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new pod",
	Long:  "create a new gpu pod",
	Args:  cobra.NoArgs,
	RunE:  runCreate,
}

var (
	createName              string
	createImageName         string
	createGpuTypeIDs        string
	createGpuCount          int
	createVolumeInGb        int
	createContainerDiskInGb int
	createVolumeMountPath   string
	createPorts             string
	createEnv               string
	createCloudType         string
	createDataCenterIDs     string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "pod name")
	createCmd.Flags().StringVar(&createImageName, "image", "", "docker image name (required)")
	createCmd.Flags().StringVar(&createGpuTypeIDs, "gpu-type-ids", "", "comma-separated list of gpu type ids")
	createCmd.Flags().IntVar(&createGpuCount, "gpu-count", 1, "number of gpus")
	createCmd.Flags().IntVar(&createVolumeInGb, "volume-in-gb", 0, "volume size in gb")
	createCmd.Flags().IntVar(&createContainerDiskInGb, "container-disk-in-gb", 20, "container disk size in gb")
	createCmd.Flags().StringVar(&createVolumeMountPath, "volume-mount-path", "/workspace", "volume mount path")
	createCmd.Flags().StringVar(&createPorts, "ports", "", "comma-separated list of ports (e.g., '8888/http,22/tcp')")
	createCmd.Flags().StringVar(&createEnv, "env", "", "environment variables as json object")
	createCmd.Flags().StringVar(&createCloudType, "cloud-type", "SECURE", "cloud type (SECURE or COMMUNITY)")
	createCmd.Flags().StringVar(&createDataCenterIDs, "data-center-ids", "", "comma-separated list of data center ids")

	createCmd.MarkFlagRequired("image") //nolint:errcheck
}

func runCreate(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.PodCreateRequest{
		Name:              createName,
		ImageName:         createImageName,
		GpuCount:          createGpuCount,
		VolumeInGb:        createVolumeInGb,
		ContainerDiskInGb: createContainerDiskInGb,
		VolumeMountPath:   createVolumeMountPath,
		CloudType:         createCloudType,
	}

	if createGpuTypeIDs != "" {
		req.GpuTypeIDs = strings.Split(createGpuTypeIDs, ",")
	}

	if createPorts != "" {
		req.Ports = strings.Split(createPorts, ",")
	}

	if createDataCenterIDs != "" {
		req.DataCenterIDs = strings.Split(createDataCenterIDs, ",")
	}

	if createEnv != "" {
		var env map[string]string
		if err := json.Unmarshal([]byte(createEnv), &env); err != nil {
			return fmt.Errorf("invalid env json: %w", err)
		}
		req.Env = env
	}

	pod, err := client.CreatePod(req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to create pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pod, &output.Config{Format: format})
}
