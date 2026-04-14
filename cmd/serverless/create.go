package serverless

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new endpoint",
	Long:  "create a new serverless endpoint",
	Args:  cobra.NoArgs,
	RunE:  runCreate,
}

var (
	createName             string
	createTemplateID       string
	createComputeType      string
	createGpuTypeID        string
	createGpuCount         int
	createWorkersMin       int
	createWorkersMax       int
	createDataCenterIDs    string
	createNetworkVolumeID  string
	createMinCudaVersion   string
	createScaleBy          string
	createScaleThreshold   int
	createIdleTimeout      int
	createFlashBoot        bool
	createExecutionTimeout int
	createNetworkVolumeIDs string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "endpoint name")
	createCmd.Flags().StringVar(&createTemplateID, "template-id", "", "template id (required)")
	createCmd.Flags().StringVar(&createComputeType, "compute-type", "GPU", "compute type (GPU or CPU)")
	createCmd.Flags().StringVar(&createGpuTypeID, "gpu-id", "", "gpu id (from 'runpodctl gpu list')")
	createCmd.Flags().IntVar(&createGpuCount, "gpu-count", 1, "number of gpus per worker")
	createCmd.Flags().IntVar(&createWorkersMin, "workers-min", 0, "minimum number of workers")
	createCmd.Flags().IntVar(&createWorkersMax, "workers-max", 3, "maximum number of workers")
	createCmd.Flags().StringVar(&createDataCenterIDs, "data-center-ids", "", "comma-separated list of data center ids")
	createCmd.Flags().StringVar(&createNetworkVolumeID, "network-volume-id", "", "network volume id to attach")
	createCmd.Flags().StringVar(&createMinCudaVersion, "min-cuda-version", "", "minimum cuda version (e.g., 12.6)")
	createCmd.Flags().StringVar(&createScaleBy, "scale-by", "", "autoscale strategy: delay (seconds of queue wait) or requests (pending request count)")
	createCmd.Flags().IntVar(&createScaleThreshold, "scale-threshold", -1, "trigger point for autoscaler (delay: seconds, requests: count)")
	createCmd.Flags().IntVar(&createIdleTimeout, "idle-timeout", -1, "seconds before idle worker scales down (1-3600)")
	createCmd.Flags().BoolVar(&createFlashBoot, "flash-boot", true, "enable flash boot")
	createCmd.Flags().IntVar(&createExecutionTimeout, "execution-timeout", -1, "max seconds per request")
	createCmd.Flags().StringVar(&createNetworkVolumeIDs, "network-volume-ids", "", "comma-separated network volume ids for multi-region")

	createCmd.MarkFlagRequired("template-id") //nolint:errcheck
}

func runCreate(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.EndpointCreateRequest{
		Name:        createName,
		TemplateID:  createTemplateID,
		ComputeType: strings.ToUpper(strings.TrimSpace(createComputeType)),
		GpuCount:    createGpuCount,
		WorkersMin:  createWorkersMin,
		WorkersMax:  createWorkersMax,
	}

	gpuTypeID := strings.TrimSpace(createGpuTypeID)
	if strings.Contains(gpuTypeID, ",") {
		return fmt.Errorf("only one gpu id is supported; use --gpu-count for multiple gpus of the same type")
	}
	if gpuTypeID != "" {
		req.GpuTypeIDs = []string{gpuTypeID}
	}

	if createNetworkVolumeID != "" {
		req.NetworkVolumeID = createNetworkVolumeID
	}

	if createDataCenterIDs != "" {
		req.DataCenterIDs = strings.Split(createDataCenterIDs, ",")
	}

	if createMinCudaVersion != "" {
		req.MinCudaVersion = createMinCudaVersion
	}

	if createScaleBy != "" {
		switch strings.ToLower(strings.TrimSpace(createScaleBy)) {
		case "delay":
			req.ScalerType = "QUEUE_DELAY"
		case "requests":
			req.ScalerType = "REQUEST_COUNT"
		default:
			return fmt.Errorf("invalid --scale-by %q (use delay or requests)", createScaleBy)
		}
	}

	if createScaleThreshold >= 0 {
		req.ScalerValue = createScaleThreshold
	}

	if createIdleTimeout >= 0 {
		if createIdleTimeout < 1 || createIdleTimeout > 3600 {
			return fmt.Errorf("--idle-timeout must be between 1 and 3600 seconds")
		}
		req.IdleTimeout = createIdleTimeout
	}

	if createExecutionTimeout >= 0 {
		req.ExecutionTimeoutMs = createExecutionTimeout * 1000
	}

	if createNetworkVolumeIDs != "" {
		req.NetworkVolumeIDs = strings.Split(createNetworkVolumeIDs, ",")
	}

	endpoint, err := client.CreateEndpoint(req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to create endpoint: %w", err)
	}

	// rest create ignores flashboot=false, so patch immediately after create
	if !createFlashBoot {
		fb := false
		_, err := client.UpdateEndpoint(endpoint.ID, &api.EndpointUpdateRequest{Flashboot: &fb})
		if err != nil {
			output.Error(err)
			return fmt.Errorf("endpoint created but failed to disable flashboot: %w", err)
		}
		endpoint.Flashboot = &fb
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}
