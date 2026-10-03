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
	deployCost        float32
	dataCenterId      string
	dockerArgs        string
	env               []string
	gpuCount          int
	gpuTypeId         string
	imageName         string
	computeType       string
	minMemoryInGb     int
	minVcpuCount      int
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

		if err := rejectUnsupportedCreateFlags(cmd); err != nil {
			cobra.CheckErr(err)
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
	CreatePodCmd.Flags().Float32Var(&deployCost, "cost", 0, "no longer supported (rest v2 cannot enforce a price ceiling)")
	CreatePodCmd.Flags().StringVar(&dockerArgs, "args", "", "container arguments")
	CreatePodCmd.Flags().StringSliceVar(&env, "env", nil, "container arguments")
	CreatePodCmd.Flags().IntVar(&gpuCount, "gpuCount", 1, "number of GPUs for the pod")
	CreatePodCmd.Flags().StringVar(&gpuTypeId, "gpuType", "", "gpu type id, e.g. 'NVIDIA GeForce RTX 3090'")
	CreatePodCmd.Flags().StringVar(&imageName, "imageName", "", "container image name")
	CreatePodCmd.Flags().StringVar(&computeType, "computeType", "GPU", "compute type (GPU or CPU)")
	CreatePodCmd.Flags().IntVar(&minMemoryInGb, "mem", 20, "minimum system memory needed")
	CreatePodCmd.Flags().IntVar(&minVcpuCount, "vcpu", 1, "minimum vCPUs needed")
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

// rejectUnsupportedCreateFlags handles the podFindAndDeployOnDemand filters
// rest v2 has no field for. a price ceiling that cannot be enforced must not be
// ignored, so --cost is refused; --mem and --vcpu were minimums the scheduler
// matched machines against, while v2 sizes a pod by its gpu, so they only warn.
func rejectUnsupportedCreateFlags(cmd *cobra.Command) error {
	if cmd.Flags().Changed("cost") {
		return fmt.Errorf("--cost is no longer supported; use 'runpodctl gpu list' to check prices, then 'runpodctl pod create'")
	}
	if volumeInGb > 0 && volumeInGb < api.MinPodVolumeInGb && networkVolumeId == "" {
		fmt.Fprintf(os.Stderr, "note: volume raised from %d gb to the minimum of %d gb\n", volumeInGb, api.MinPodVolumeInGb)
	}
	for _, flag := range []string{"mem", "vcpu"} {
		if cmd.Flags().Changed(flag) {
			fmt.Fprintf(os.Stderr, "warning: --%s is no longer applied; memory and vcpus are sized by the gpu type\n", flag)
		}
	}
	return nil
}
