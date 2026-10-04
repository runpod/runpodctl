package pods

import (
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/cobra"
)

var (
	communityCloud    bool
	containerDiskInGb int
	dockerArgs        string
	env               []string
	gpuCount          int
	gpuTypeId         string
	imageName         string
	name              string
	podCount          int
	ports             []string
	secureCloud       bool
	templateId        string
	volumeInGb        int
	volumeMountPath   string
)

var CreatePodsCmd = &cobra.Command{
	Use:   "pods",
	Args:  cobra.ExactArgs(0),
	Short: "create a group of pods",
	Long:  "create a group of pods on runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		gpus := strings.Split(gpuTypeId, ",")
		gpusIndex := 0
		input := &api.LegacyPodCreate{
			ContainerDiskInGb: containerDiskInGb,
			DockerArgs:        dockerArgs,
			GpuCount:          gpuCount,
			ImageName:         imageName,
			Name:              name,
			TemplateID:        templateId,
			VolumeInGb:        volumeInGb,
			VolumeMountPath:   volumeMountPath,
		}
		if len(ports) > 0 {
			input.Ports = ports
		}
		input.Env = make(map[string]string, len(env))
		for _, v := range env {
			e := strings.Split(v, "=")
			if len(e) != 2 {
				cobra.CheckErr(fmt.Errorf("wrong env value: %s", e))
			}
			input.Env[e[0]] = e[1]
		}
		if secureCloud {
			input.CloudType = "SECURE"
		} else {
			input.CloudType = "COMMUNITY"
		}

		input.GpuTypeID = gpus[0]
		if note := input.VolumeNote(); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}

		client, err := api.NewClient()
		cobra.CheckErr(err)
		for x := 0; x < podCount; x++ {
			input.GpuTypeID = gpus[gpusIndex]
			pod, _, err := client.CreatePodV2(input.V2Request())
			if err != nil && len(gpus) > gpusIndex+1 && strings.Contains(err.Error(), "no longer any instances available") {
				gpusIndex++
				x--
				continue
			}
			cobra.CheckErr(err)

			if pod.DesiredStatus == "RUNNING" {
				fmt.Printf(`pod "%s" created for $%.3f / hr`, pod.ID, pod.CostPerHr)
				fmt.Println()
			} else {
				cobra.CheckErr(fmt.Errorf(`pod %v start failed; status is %v`, pod.ID, pod.DesiredStatus))
			}
		}
	},
}

func init() {
	CreatePodsCmd.Flags().BoolVar(&communityCloud, "communityCloud", false, "create in community cloud")
	CreatePodsCmd.Flags().BoolVar(&secureCloud, "secureCloud", false, "create in secure cloud")
	CreatePodsCmd.Flags().IntVar(&containerDiskInGb, "containerDiskSize", 20, "container disk size in GB")
	CreatePodsCmd.Flags().IntVar(&gpuCount, "gpuCount", 1, "number of GPUs for the pod")
	CreatePodsCmd.Flags().IntVar(&podCount, "podCount", 1, "number of pods to create with the same name")
	CreatePodsCmd.Flags().IntVar(&volumeInGb, "volumeSize", 1, "persistent volume disk size in GB")
	CreatePodsCmd.Flags().StringSliceVar(&env, "env", nil, "container arguments")
	CreatePodsCmd.Flags().StringSliceVar(&ports, "ports", nil, "ports to expose; max only 1 http and 1 tcp allowed; e.g. '8888/http'")
	CreatePodsCmd.Flags().StringVar(&dockerArgs, "args", "", "container arguments")
	CreatePodsCmd.Flags().StringVar(&gpuTypeId, "gpuType", "", "gpu type id, e.g. 'NVIDIA GeForce RTX 3090'")
	CreatePodsCmd.Flags().StringVar(&imageName, "imageName", "", "container image name")
	CreatePodsCmd.Flags().StringVar(&name, "name", "", "any pod name for easy reference")
	CreatePodsCmd.Flags().StringVar(&templateId, "templateId", "", "templateId to use with the pods")
	CreatePodsCmd.Flags().StringVar(&volumeMountPath, "volumePath", "/runpod", "container volume path")

	CreatePodsCmd.MarkFlagRequired("gpuType")   //nolint
	CreatePodsCmd.MarkFlagRequired("imageName") //nolint
	CreatePodsCmd.MarkFlagRequired("name")      //nolint
}
