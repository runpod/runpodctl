package pods

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var (
 communityCloud bool
 containerDiskInGb int
 deployCost float32
 dockerArgs string
 env []string
 gpuCount int
 gpuTypeId string
 imageName string
 minMemoryInGb int
 minVcpuCount int
 name string
 podCount int
 ports []string
 secureCloud bool
 templateId string
 volumeInGb int
 volumeMountPath string
)

var CreatePodsCmd = &cobra.Command{
	Use:   "pods",
	Args:  cobra.ExactArgs(0),
	Short: "create a group of pods",
	Long:  "create a group of pods on runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		gpus := strings.Split(gpuTypeId, ",")
		gpusIndex := 0
		input := &api.CreatePodInput{
			ContainerDiskInGb: containerDiskInGb,
			DeployCost:        deployCost,
			DockerArgs:        dockerArgs,
			GpuCount:          gpuCount,
			ImageName:         imageName,
			MinMemoryInGb:     minMemoryInGb,
			MinVcpuCount:      minVcpuCount,
			Name:              name,
			TemplateId:        templateId,
			VolumeInGb:        volumeInGb,
			VolumeMountPath:   volumeMountPath,
		}
		if len(ports) > 0 {
			input.Ports = strings.Join(ports, ",")
		}
		input.Env = make([]*api.PodEnv, len(env))
		for i, v := range env {
			e := strings.Split(v, "=")
			if len(e) != 2 {
				cobra.CheckErr(fmt.Errorf("wrong env value: %s", e))
			}
			input.Env[i] = &api.PodEnv{Key: e[0], Value: e[1]}
		}
		if secureCloud {
			input.CloudType = "SECURE"
		} else {
			input.CloudType = "COMMUNITY"
		}

		for x := 0; x < podCount; x++ {
			input.GpuTypeId = gpus[gpusIndex]
			pod, err := api.CreatePod(input)
			if err != nil && len(gpus) > gpusIndex+1 && strings.Contains(err.Error(), "no longer any instances available") {
				gpusIndex++
				x--
				continue
			}
			cobra.CheckErr(err)

			if pod["desiredStatus"] == "RUNNING" {
				fmt.Printf(`pod "%s" created for $%.3f / hr`, pod["id"], pod["costPerHr"])
				fmt.Println()
			} else {
				cobra.CheckErr(fmt.Errorf(`pod "%s" start failed; status is %s`, args[0], pod["desiredStatus"]))
			}
		}
	},
}

func init() {
	CreatePodsCmd.Flags().BoolVar(&communityCloud, "communityCloud", false, "create in community cloud")
	CreatePodsCmd.Flags().BoolVar(&secureCloud, "secureCloud", false, "create in secure cloud")
	CreatePodsCmd.Flags().Float32Var(&deployCost, "cost", 0, "$/hr price ceiling, if not defined, pod will be created with lowest price available")
	CreatePodsCmd.Flags().IntVar(&containerDiskInGb, "containerDiskSize", 20, "container disk size in GB")
	CreatePodsCmd.Flags().IntVar(&gpuCount, "gpuCount", 1, "number of GPUs for the pod")
	CreatePodsCmd.Flags().IntVar(&minMemoryInGb, "mem", 20, "minimum system memory needed")
	CreatePodsCmd.Flags().IntVar(&minVcpuCount, "vcpu", 1, "minimum vCPUs needed")
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
