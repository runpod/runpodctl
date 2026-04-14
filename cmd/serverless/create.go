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
--hub-id accepts both SERVERLESS and POD hub listings.

examples:
  # create from a template
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # create from a hub repo
  runpodctl hub search vllm                         # find the hub id
  runpodctl serverless create --hub-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # override or add env vars (hub defaults are included automatically)
  runpodctl serverless create --hub-id <id> --env MODEL_NAME=my-model --env MAX_TOKENS=4096`,
	Args: cobra.NoArgs,
	RunE: runCreate,
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
	createEnvVars          []string
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
	createCmd.Flags().StringVar(&createTemplateID, "template-id", "", "template id (required if no --hub-id)")
	createCmd.Flags().StringVar(&createHubID, "hub-id", "", "hub listing id; accepts both SERVERLESS and POD types (alternative to --template-id)")
	createCmd.Flags().StringVar(&createComputeType, "compute-type", "GPU", "compute type (GPU or CPU)")
	createCmd.Flags().StringVar(&createGpuTypeID, "gpu-id", "", "gpu id (from 'runpodctl gpu list')")
	createCmd.Flags().IntVar(&createGpuCount, "gpu-count", 1, "number of gpus per worker")
	createCmd.Flags().IntVar(&createWorkersMin, "workers-min", 0, "minimum number of workers")
	createCmd.Flags().IntVar(&createWorkersMax, "workers-max", 3, "maximum number of workers")
	createCmd.Flags().StringVar(&createDataCenterIDs, "data-center-ids", "", "comma-separated list of data center ids")
	createCmd.Flags().StringVar(&createNetworkVolumeID, "network-volume-id", "", "network volume id to attach")
	createCmd.Flags().StringSliceVar(&createEnvVars, "env", nil, "env vars in KEY=VALUE format; overrides hub defaults (repeatable)")
	createCmd.Flags().StringVar(&createMinCudaVersion, "min-cuda-version", "", "minimum cuda version (e.g., 12.6)")
	createCmd.Flags().StringVar(&createScaleBy, "scale-by", "", "autoscale strategy: delay (seconds of queue wait) or requests (pending request count)")
	createCmd.Flags().IntVar(&createScaleThreshold, "scale-threshold", -1, "trigger point for autoscaler (delay: seconds, requests: count)")
	createCmd.Flags().IntVar(&createIdleTimeout, "idle-timeout", -1, "seconds before idle worker scales down (1-3600)")
	createCmd.Flags().BoolVar(&createFlashBoot, "flash-boot", true, "enable flash boot")
	createCmd.Flags().IntVar(&createExecutionTimeout, "execution-timeout", -1, "max seconds per request")
	createCmd.Flags().StringVar(&createNetworkVolumeIDs, "network-volume-ids", "", "comma-separated network volume ids for multi-region")
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

		// translate hub release env config into pod env vars
		envMap := make(map[string]string, len(hubConfig.Env))
		envOrder := make([]string, 0, len(hubConfig.Env))
		for _, e := range hubConfig.Env {
			val := ""
			if e.Input != nil && e.Input.Default != nil {
				val = fmt.Sprintf("%v", e.Input.Default)
			}
			envMap[e.Key] = val
			envOrder = append(envOrder, e.Key)
		}

		// apply user --env overrides (take precedence over hub defaults)
		for _, kv := range createEnvVars {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid --env format %q; expected KEY=VALUE", kv)
			}
			key, val := parts[0], parts[1]
			if _, exists := envMap[key]; !exists {
				envOrder = append(envOrder, key)
			}
			envMap[key] = val
		}

		envVars := make([]*api.PodEnvVar, 0, len(envMap))
		for _, key := range envOrder {
			envVars = append(envVars, &api.PodEnvVar{Key: key, Value: envMap[key]})
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
				Env:               envVars,
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

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))] //nolint:gosec
	}
	return string(b)
}
