package pod

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new pod",
	Long: `create a new pod.

you can create a pod either from a template or by specifying an image directly.

examples:
  # create from template (recommended)
  runpodctl pod create --template-id runpod-torch-v21 --gpu-id "NVIDIA RTX 4090"

  # create with custom image
  runpodctl pod create --image runpod/pytorch:2.1.0-py3.10-cuda11.8.0-devel-ubuntu22.04 --gpu-id "NVIDIA RTX 4090"

  # create a cpu pod
  runpodctl pod create --compute-type cpu --image ubuntu:22.04

  # find templates first
  runpodctl template search pytorch
  runpodctl template list --type official`,
	Args: cobra.NoArgs,
	RunE: runCreate,
}

var (
	createName              string
	createImageName         string
	createTemplateID        string
	createComputeType       string
	createGpuTypeID         string
	createGpuCount          int
	createVolumeInGb        int
	createContainerDiskInGb int
	createVolumeMountPath   string
	createGlobalNetworking  bool
	createPublicIP          bool
	createPorts             string
	createEnv               string
	createCloudType         string
	createDataCenterIDs     string
	createSSH               bool
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "pod name")
	createCmd.Flags().StringVar(&createTemplateID, "template-id", "", "template id (use 'runpodctl template search' to find templates)")
	createCmd.Flags().StringVar(&createImageName, "image", "", "docker image name (required if no template)")
	createCmd.Flags().StringVar(&createComputeType, "compute-type", "GPU", "compute type (GPU or CPU)")
	createCmd.Flags().StringVar(&createGpuTypeID, "gpu-id", "", "gpu id (from 'runpodctl gpu list')")
	createCmd.Flags().IntVar(&createGpuCount, "gpu-count", 1, "number of gpus")
	createCmd.Flags().IntVar(&createVolumeInGb, "volume-in-gb", 0, "volume size in gb")
	createCmd.Flags().IntVar(&createContainerDiskInGb, "container-disk-in-gb", 20, "container disk size in gb")
	createCmd.Flags().StringVar(&createVolumeMountPath, "volume-mount-path", "/workspace", "volume mount path")
	createCmd.Flags().BoolVar(&createGlobalNetworking, "global-networking", false, "enable global networking (secure cloud only)")
	createCmd.Flags().BoolVar(&createPublicIP, "public-ip", false, "require public ip (community cloud only)")
	createCmd.Flags().StringVar(&createPorts, "ports", "", "comma-separated list of ports (e.g., '8888/http,22/tcp')")
	createCmd.Flags().StringVar(&createEnv, "env", "", "environment variables as json object")
	createCmd.Flags().StringVar(&createCloudType, "cloud-type", "SECURE", "cloud type (SECURE or COMMUNITY)")
	createCmd.Flags().StringVar(&createDataCenterIDs, "data-center-ids", "", "comma-separated list of data center ids")
	createCmd.Flags().BoolVar(&createSSH, "ssh", true, "enable ssh on the pod")
}

func runCreate(cmd *cobra.Command, args []string) error {
	// Validate: either template or image must be provided
	if createTemplateID == "" && createImageName == "" {
		return fmt.Errorf("either --template-id or --image is required\n\nuse 'runpodctl template search <term>' to find templates")
	}

	computeType := strings.ToUpper(strings.TrimSpace(createComputeType))
	if computeType == "" {
		computeType = "GPU"
	}
	switch computeType {
	case "GPU", "CPU":
	default:
		return fmt.Errorf("invalid --compute-type %q (use GPU or CPU)", createComputeType)
	}

	gpuTypeID := strings.TrimSpace(createGpuTypeID)
	if strings.Contains(gpuTypeID, ",") {
		return fmt.Errorf("only one gpu id is supported; use --gpu-count for multiple gpus of the same type")
	}

	if computeType == "CPU" && gpuTypeID != "" {
		return fmt.Errorf("--gpu-id is not supported for compute type CPU")
	}

	cloudType := strings.ToUpper(strings.TrimSpace(createCloudType))
	if cloudType == "" {
		cloudType = "SECURE"
	}
	if createGlobalNetworking {
		if computeType != "GPU" {
			return fmt.Errorf("global networking requires compute type GPU")
		}
		if cloudType != "SECURE" {
			return fmt.Errorf("global networking is only supported on secure cloud (set --cloud-type SECURE)")
		}
		if strings.TrimSpace(createDataCenterIDs) != "" {
			fmt.Fprintln(os.Stderr, "note: global networking availability varies by data center; if create fails, try another secure data center or omit --data-center-ids")
		}
	}

	supportPublicIP := false
	if createPublicIP {
		if cloudType == "SECURE" {
			fmt.Fprintln(os.Stderr, "note: secure cloud pods always have public ips; --public-ip has no effect")
		}
		if cloudType == "COMMUNITY" {
			supportPublicIP = true
		}
	}

	var (
		result interface{}
		err    error
	)

	if computeType == "CPU" {
		// CPU pods use the REST API (GraphQL requires gpuTypeId)
		result, err = createPodREST(computeType, gpuTypeID, cloudType, supportPublicIP)
	} else {
		// GPU pods use GraphQL (supports startSsh)
		result, err = createPodGraphQL(gpuTypeID, cloudType, supportPublicIP)
	}
	if err != nil {
		if createGlobalNetworking {
			err = decorateGlobalNetworkingError(err, createDataCenterIDs)
		}
		output.Error(err)
		return fmt.Errorf("failed to create pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(result, &output.Config{Format: format})
}

func createPodGraphQL(gpuTypeID, cloudType string, supportPublicIP bool) (map[string]interface{}, error) {
	gqlClient, err := api.NewGraphQLClient()
	if err != nil {
		return nil, err
	}

	req := &api.CreatePodGQLInput{
		CloudType:         cloudType,
		ContainerDiskInGb: createContainerDiskInGb,
		GpuCount:          createGpuCount,
		GpuTypeId:         gpuTypeID,
		ImageName:         createImageName,
		Name:              createName,
		StartSsh:          createSSH,
		SupportPublicIp:   supportPublicIP,
		TemplateId:        createTemplateID,
		VolumeInGb:        createVolumeInGb,
		VolumeMountPath:   createVolumeMountPath,
	}

	if createPorts != "" {
		req.Ports = createPorts
	}

	// GraphQL only supports a single dataCenterId
	if createDataCenterIDs != "" {
		ids := strings.Split(createDataCenterIDs, ",")
		req.DataCenterId = strings.TrimSpace(ids[0])
		if len(ids) > 1 {
			fmt.Fprintln(os.Stderr, "note: only the first data center id is used; graphql api supports a single data center")
		}
	}

	if createEnv != "" {
		var envMap map[string]string
		if err := json.Unmarshal([]byte(createEnv), &envMap); err != nil {
			return nil, fmt.Errorf("invalid env json: %w", err)
		}
		for k, v := range envMap {
			req.Env = append(req.Env, &api.PodEnvVar{Key: k, Value: v})
		}
	}

	return gqlClient.CreatePod(req)
}

func createPodREST(computeType, gpuTypeID, cloudType string, supportPublicIP bool) (*api.Pod, error) {
	client, err := api.NewClient()
	if err != nil {
		return nil, err
	}

	req := &api.PodCreateRequest{
		Name:              createName,
		ImageName:         createImageName,
		TemplateID:        createTemplateID,
		ComputeType:       computeType,
		GlobalNetworking:  createGlobalNetworking,
		SupportPublicIp:   supportPublicIP,
		GpuCount:          0,
		VolumeInGb:        createVolumeInGb,
		ContainerDiskInGb: createContainerDiskInGb,
		VolumeMountPath:   createVolumeMountPath,
		CloudType:         cloudType,
	}

	if gpuTypeID != "" {
		req.GpuTypeIDs = []string{gpuTypeID}
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
			return nil, fmt.Errorf("invalid env json: %w", err)
		}
		req.Env = env
	}

	return client.CreatePod(req)
}

func decorateGlobalNetworkingError(err error, dataCenterIDs string) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "global networking") && !strings.Contains(msg, "globalnetworking") {
		return err
	}

	hint := "global networking is only available for on-demand GPU pods in some secure cloud data centers"
	if strings.TrimSpace(dataCenterIDs) != "" {
		hint += "; try another secure data center or omit --data-center-ids"
	}
	return fmt.Errorf("%s: %w", hint, err)
}
