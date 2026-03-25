package serverless

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new endpoint",
	Long: `create a new serverless endpoint.

requires either --template-id or --hub-id.

examples:
  # create from a template
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # create from a hub repo
  runpodctl hub search vllm                         # find the hub id
  runpodctl serverless create --hub-id <id> --gpu-id "NVIDIA GeForce RTX 4090"`,
	Args:  cobra.NoArgs,
	RunE:  runCreate,
}

var (
	createName             string
	createTemplateID       string
	createHubID            string
	createComputeType      string
	createGpuTypeID        string
	createGpuCount         int
	createWorkersMin       int
	createWorkersMax       int
	createDataCenterIDs    string
	createNetworkVolumeID  string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "endpoint name")
	createCmd.Flags().StringVar(&createTemplateID, "template-id", "", "template id (required if no --hub-id)")
	createCmd.Flags().StringVar(&createHubID, "hub-id", "", "hub listing id (alternative to --template-id)")
	createCmd.Flags().StringVar(&createComputeType, "compute-type", "GPU", "compute type (GPU or CPU)")
	createCmd.Flags().StringVar(&createGpuTypeID, "gpu-id", "", "gpu id (from 'runpodctl gpu list')")
	createCmd.Flags().IntVar(&createGpuCount, "gpu-count", 1, "number of gpus per worker")
	createCmd.Flags().IntVar(&createWorkersMin, "workers-min", 0, "minimum number of workers")
	createCmd.Flags().IntVar(&createWorkersMax, "workers-max", 3, "maximum number of workers")
	createCmd.Flags().StringVar(&createDataCenterIDs, "data-center-ids", "", "comma-separated list of data center ids")
	createCmd.Flags().StringVar(&createNetworkVolumeID, "network-volume-id", "", "network volume id to attach")

}

func runCreate(cmd *cobra.Command, args []string) error {
	if createTemplateID == "" && createHubID == "" {
		return fmt.Errorf("either --template-id or --hub-id is required\n\nuse 'runpodctl hub search <term>' to find hub repos\nuse 'runpodctl template search <term>' to find templates")
	}
	if createTemplateID != "" && createHubID != "" {
		return fmt.Errorf("--template-id and --hub-id are mutually exclusive; use one or the other")
	}

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

	// hub-id path: resolve listing, create via graphql (REST api doesn't support hubReleaseId)
	if createHubID != "" {
		listing, err := client.GetListing(createHubID)
		if err != nil {
			output.Error(err)
			return fmt.Errorf("failed to get hub listing: %w", err)
		}
		if listing.ListedRelease == nil {
			return fmt.Errorf("hub listing %q has no published release", createHubID)
		}

		release := listing.ListedRelease

		// build inline template from the hub release (same as web ui)
		var imageName string
		if release.Build != nil {
			imageName = release.Build.ImageName
		}
		if imageName == "" {
			return fmt.Errorf("hub listing %q has no built image; the release may still be building", createHubID)
		}

		containerDisk := 10
		var hubConfig api.HubReleaseConfig
		if release.Config != "" {
			if err := json.Unmarshal([]byte(release.Config), &hubConfig); err == nil {
				if hubConfig.ContainerDiskInGb > 0 {
					containerDisk = hubConfig.ContainerDiskInGb
				}
			}
		}

		endpointName := createName
		if endpointName == "" {
			endpointName = listing.Title
		}

		//nolint:gosec
		templateName := fmt.Sprintf("%s__template__%s", endpointName, randomString(7))

		gqlReq := &api.EndpointCreateGQLInput{
			Name:         endpointName,
			HubReleaseID: release.ID,
			Template: &api.EndpointTemplateInput{
				Name:              templateName,
				ImageName:         imageName,
				ContainerDiskInGb: containerDisk,
				DockerArgs:        "",
				Env:               []*api.PodEnvVar{},
			},
			GpuCount:   createGpuCount,
			WorkersMin: createWorkersMin,
			WorkersMax: createWorkersMax,
		}

		// use gpu ids from hub config if not explicitly provided
		if gpuTypeID != "" {
			gqlReq.GpuIDs = gpuTypeID
		} else if hubConfig.GpuIDs != "" {
			gqlReq.GpuIDs = hubConfig.GpuIDs
		}
		if createNetworkVolumeID != "" {
			gqlReq.NetworkVolumeID = createNetworkVolumeID
		}
		if createDataCenterIDs != "" {
			gqlReq.Locations = createDataCenterIDs
		}

		endpoint, err := client.CreateEndpointGQL(gqlReq)
		if err != nil {
			output.Error(err)
			return fmt.Errorf("failed to create endpoint: %w", err)
		}

		format := output.ParseFormat(cmd.Flag("output").Value.String())
		return output.Print(endpoint, &output.Config{Format: format})
	}

	// template-id path: create via REST
	if gpuTypeID != "" {
		req.GpuTypeIDs = []string{gpuTypeID}
	}

	if createNetworkVolumeID != "" {
		req.NetworkVolumeID = createNetworkVolumeID
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

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))] //nolint:gosec
	}
	return string(b)
}
