package pod

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var (
	communityCloud    bool
	secureCloud       bool
	containerDiskInGb int
	deployCost        float32
	dockerArgs        string
	env               []string
	gpuCount          int
	gpuTypeId         string
	imageName         string
	minMemoryInGb     int
	minVcpuCount      int
	name              string
	ports             []string
	templateId        string
	volumeInGb        int
	volumeMountPath   string
)

var CreatePodCmd = &cobra.Command{
	Use:   "pod",
	Args:  cobra.ExactArgs(0),
	Short: "start a pod",
	Long:  "start a pod from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		input := &api.CreatePodInput{
			ContainerDiskInGb: containerDiskInGb,
			DeployCost:        deployCost,
			DockerArgs:        dockerArgs,
			GpuCount:          gpuCount,
			GpuTypeId:         gpuTypeId,
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
		pod, err := api.CreatePod(input)
		cobra.CheckErr(err)

		if pod["desiredStatus"] == "RUNNING" {
			fmt.Printf(`pod "%s" created for $%.3f / hr`, pod["id"], pod["costPerHr"])
			fmt.Println()
		} else {
			cobra.CheckErr(fmt.Errorf(`pod "%s" start failed; status is %s`, args[0], pod["desiredStatus"]))
		}
	},
}

func init() {
	CreatePodCmd.Flags().BoolVar(&communityCloud, "communityCloud", false, "create in community cloud")
	CreatePodCmd.Flags().BoolVar(&secureCloud, "secureCloud", false, "create in secure cloud")
	CreatePodCmd.Flags().IntVar(&containerDiskInGb, "containerDiskSize", 20, "container disk size in GB")
	CreatePodCmd.Flags().Float32Var(&deployCost, "cost", 0, "$/hr price ceiling, if not defined, pod will be created with lowest price available")
	CreatePodCmd.Flags().StringVar(&dockerArgs, "args", "", "container arguments")
	CreatePodCmd.Flags().StringSliceVar(&env, "env", nil, "container arguments")
	CreatePodCmd.Flags().IntVar(&gpuCount, "gpuCount", 1, "number of GPUs for the pod")
	CreatePodCmd.Flags().StringVar(&gpuTypeId, "gpuType", "", "gpu type id, e.g. 'NVIDIA GeForce RTX 3090'")
	CreatePodCmd.Flags().StringVar(&imageName, "imageName", "", "container image name")
	CreatePodCmd.Flags().IntVar(&minMemoryInGb, "mem", 20, "minimum system memory needed")
	CreatePodCmd.Flags().IntVar(&minVcpuCount, "vcpu", 1, "minimum vCPUs needed")
	CreatePodCmd.Flags().StringVar(&name, "name", "", "any pod name for easy reference")
	CreatePodCmd.Flags().StringSliceVar(&ports, "ports", nil, "ports to expose; max only 1 http and 1 tcp allowed; e.g. '8888/http'")
	CreatePodCmd.Flags().StringVar(&templateId, "templateId", "", "templateId to use with the pod")
	CreatePodCmd.Flags().IntVar(&volumeInGb, "volumeSize", 1, "persistent volume disk size in GB")
	CreatePodCmd.Flags().StringVar(&volumeMountPath, "volumePath", "/runpod", "container volume path")

	CreatePodCmd.MarkFlagRequired("gpuType")   //nolint
	CreatePodCmd.MarkFlagRequired("imageName") //nolint
}
