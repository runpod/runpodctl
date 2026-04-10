package pod

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <pod-id>",
	Short: "update an existing pod",
	Long:  "update an existing pod's configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

var (
	updateName              string
	updateImageName         string
	updateContainerDiskInGb int
	updateVolumeInGb        int
	updateVolumeMountPath   string
	updatePorts             string
	updateEnv               string
)

func init() {
	updateCmd.Flags().StringVar(&updateName, "name", "", "new pod name")
	updateCmd.Flags().StringVar(&updateImageName, "image", "", "new docker image name")
	updateCmd.Flags().IntVar(&updateContainerDiskInGb, "container-disk-in-gb", 0, "new container disk size in gb")
	updateCmd.Flags().IntVar(&updateVolumeInGb, "volume-in-gb", 0, "new volume size in gb")
	updateCmd.Flags().StringVar(&updateVolumeMountPath, "volume-mount-path", "", "new volume mount path")
	updateCmd.Flags().StringVar(&updatePorts, "ports", "", "new comma-separated list of ports")
	updateCmd.Flags().StringVar(&updateEnv, "env", "", "new environment variables as json object")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	podID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.PodUpdateRequest{}

	if updateName != "" {
		req.Name = updateName
	}
	if updateImageName != "" {
		req.ImageName = updateImageName
	}
	if updateContainerDiskInGb > 0 {
		req.ContainerDiskInGb = updateContainerDiskInGb
	}
	if updateVolumeInGb > 0 {
		req.VolumeInGb = updateVolumeInGb
	}
	if updateVolumeMountPath != "" {
		req.VolumeMountPath = updateVolumeMountPath
	}
	if updatePorts != "" {
		req.Ports = strings.Split(updatePorts, ",")
	}
	if updateEnv != "" {
		env, err := parseUpdateEnv(updateEnv)
		if err != nil {
			return fmt.Errorf("invalid env json: %w", err)
		}
		pod, err := client.GetPod(podID, false, false)
		if err != nil {
			output.Error(err)
			return fmt.Errorf("failed to get existing pod env: %w", err)
		}
		req.Env = mergeEnvMaps(pod.Env, env)
	}

	pod, err := client.UpdatePod(podID, req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to update pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(pod, &output.Config{Format: format})
}

func parseUpdateEnv(raw string) (map[string]string, error) {
	var env map[string]string
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, err
	}
	return env, nil
}

func mergeEnvMaps(existing, updates map[string]string) map[string]string {
	if len(existing) == 0 && len(updates) == 0 {
		return nil
	}

	merged := make(map[string]string, len(existing)+len(updates))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range updates {
		merged[k] = v
	}
	return merged
}
