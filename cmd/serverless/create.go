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
	createName          string
	createTemplateID    string
	createComputeType   string
	createGpuTypeID     string
	createGpuCount      int
	createWorkersMin    int
	createWorkersMax    int
	createDataCenterIDs string
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

	if createDataCenterIDs != "" {
		req.DataCenterIDs = strings.Split(createDataCenterIDs, ",")
	}

	endpoint, err := client.CreateEndpoint(req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to create endpoint: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}
