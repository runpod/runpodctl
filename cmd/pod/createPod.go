package pod

import (
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpodctl/internal/api"

	"github.com/spf13/cobra"
)

var (
	communityCloud    bool
	secureCloud       bool
	containerDiskInGb int
	dataCenterId      string
	dockerArgs        string
	env               []string
	gpuCount          int
	gpuTypeId         string
	imageName         string
	computeType       string
	name              string
	ports             []string
	startSSH          bool
	templateId        string
	volumeInGb        int
	volumeMountPath   string
	networkVolumeId   string
)

var CreatePodCmd = &cobra.Command{
	Use:   "pod",
	Args:  cobra.ExactArgs(0),
	Short: "start a pod",
	Long:  "start a pod from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		ct := strings.ToUpper(strings.TrimSpace(computeType))
		if ct == "" {
			ct = "GPU"
		}
		switch ct {
		case "GPU", "CPU":
		default:
			cobra.CheckErr(fmt.Errorf("invalid computeType %q (use GPU or CPU)", computeType))
		}
		if ct == "CPU" {
			if gpuTypeId != "" {
				cobra.CheckErr(fmt.Errorf("gpuType must be empty when computeType is CPU"))
			}
			gpuCount = 0
		} else if gpuTypeId == "" {
			cobra.CheckErr(fmt.Errorf("gpuType is required for GPU pods"))
		}

		input := &api.LegacyPodCreate{
			ContainerDiskInGb: containerDiskInGb,
			DataCenterID:      dataCenterId,
			DockerArgs:        dockerArgs,
			GpuCount:          gpuCount,
			GpuTypeID:         gpuTypeId,
			ImageName:         imageName,
			Name:              name,
			StartSSH:          startSSH,
			TemplateID:        templateId,
			VolumeInGb:        volumeInGb,
			VolumeMountPath:   volumeMountPath,
			NetworkVolumeID:   networkVolumeId,
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

		if note := input.VolumeNote(); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
		if ct == "CPU" && volumeInGb > 0 && networkVolumeId == "" && cmd.Flags().Changed("volumeSize") {
			fmt.Fprintln(os.Stderr, "note: cpu pods cannot have a pod volume; creating without one")
		}

		client, err := api.NewClient()
		cobra.CheckErr(err)
		pod, _, err := client.CreatePodV2(input.V2Request())
		cobra.CheckErr(err)

		if pod.DesiredStatus == "RUNNING" {
			fmt.Printf(`pod "%s" created for $%.3f / hr`, pod.ID, pod.CostPerHr)
			fmt.Println()
		} else {
			cobra.CheckErr(fmt.Errorf(`pod %v start failed; status is %v`, pod.ID, pod.DesiredStatus))
		}
	},
}

func init() {
	CreatePodCmd.Flags().BoolVar(&communityCloud, "communityCloud", false, "create in community cloud")
	CreatePodCmd.Flags().BoolVar(&secureCloud, "secureCloud", false, "create in secure cloud")
	CreatePodCmd.Flags().IntVar(&containerDiskInGb, "containerDiskSize", 20, "container disk size in GB")
	CreatePodCmd.Flags().StringVar(&dockerArgs, "args", "", "container arguments")
	CreatePodCmd.Flags().StringSliceVar(&env, "env", nil, "container arguments")
	CreatePodCmd.Flags().IntVar(&gpuCount, "gpuCount", 1, "number of GPUs for the pod")
	CreatePodCmd.Flags().StringVar(&gpuTypeId, "gpuType", "", "gpu type id, e.g. 'NVIDIA GeForce RTX 3090'")
	CreatePodCmd.Flags().StringVar(&imageName, "imageName", "", "container image name")
	CreatePodCmd.Flags().StringVar(&computeType, "computeType", "GPU", "compute type (GPU or CPU)")
	CreatePodCmd.Flags().StringVar(&name, "name", "", "any pod name for easy reference")
	CreatePodCmd.Flags().StringSliceVar(&ports, "ports", nil, "ports to expose; max only 1 http and 1 tcp allowed; e.g. '8888/http'")
	CreatePodCmd.Flags().StringVar(&templateId, "templateId", "", "templateId to use with the pod")
	CreatePodCmd.Flags().IntVar(&volumeInGb, "volumeSize", 1, "persistent volume disk size in GB")
	CreatePodCmd.Flags().StringVar(&volumeMountPath, "volumePath", "/runpod", "container volume path")
	CreatePodCmd.Flags().StringVar(&networkVolumeId, "networkVolumeId", "", "network volume id")
	CreatePodCmd.Flags().StringVar(&dataCenterId, "dataCenterId", "", "datacenter id to create in")
	CreatePodCmd.Flags().BoolVar(&startSSH, "startSSH", false, "enable SSH login")

	CreatePodCmd.MarkFlagRequired("imageName") //nolint
}
