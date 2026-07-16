package serverless

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

// defaultCPUInstanceID is the cpu flavor used when --compute-type CPU is set
// without an explicit --instance-id. matches the hub deploy default server-side.
const defaultCPUInstanceID = "cpu3g-4-16"

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new endpoint",
	Long: `create a new serverless endpoint.

requires either --template-id or --hub-id.
--hub-id accepts both SERVERLESS and POD hub listings.

examples:
  # create from a template
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # create from a template and attach a model
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090" --model-reference https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct:main

  # create a cpu endpoint
  runpodctl serverless create --template-id <id> --compute-type CPU

  # create from a hub repo
  runpodctl hub search vllm                         # find the hub id
  runpodctl serverless create --hub-id <id> --gpu-id "NVIDIA GeForce RTX 4090"

  # create from a hub repo and attach a model
  runpodctl serverless create --hub-id <id> --gpu-id "NVIDIA GeForce RTX 4090" --model-reference https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct:main

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
	createInstanceID       string
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
	createModelReferences  []string
)

func init() {
	createCmd.Flags().StringVar(&createName, "name", "", "endpoint name")
	createCmd.Flags().StringVar(&createTemplateID, "template-id", "", "template id (required if no --hub-id)")
	createCmd.Flags().StringVar(&createHubID, "hub-id", "", "hub listing id; accepts both SERVERLESS and POD types (alternative to --template-id)")
	createCmd.Flags().StringVar(&createComputeType, "compute-type", "GPU", "compute type (GPU or CPU)")
	createCmd.Flags().StringVar(&createGpuTypeID, "gpu-id", "", "gpu id (from 'runpodctl gpu list')")
	createCmd.Flags().IntVar(&createGpuCount, "gpu-count", 1, "number of gpus per worker")
	createCmd.Flags().StringVar(&createInstanceID, "instance-id", "", "cpu instance id for --compute-type CPU (e.g. cpu3g-4-16)")
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
	createCmd.Flags().StringArrayVar(&createModelReferences, "model-reference", nil, "hugging face model url with a ref to cache on the endpoint, e.g. https://huggingface.co/<org>/<model>:main; works with --template-id or --hub-id, gpu only (repeatable)")
}

func runCreate(cmd *cobra.Command, args []string) error {
	if createTemplateID == "" && createHubID == "" {
		return fmt.Errorf("either --template-id or --hub-id is required\n\nuse 'runpodctl hub search <term>' to find hub repos\nuse 'runpodctl template search <term>' to find templates")
	}
	if createTemplateID != "" && createHubID != "" {
		return fmt.Errorf("--template-id and --hub-id are mutually exclusive; use one or the other")
	}
	if name := strings.TrimSpace(createName); name != "" && len(name) < 3 {
		return fmt.Errorf("--name must be at least 3 characters")
	}
	if createScaleThreshold >= 0 && createScaleThreshold < 1 {
		// the server rejects a scaler value below 1; catch it with a clear message.
		return fmt.Errorf("--scale-threshold must be at least 1")
	}

	computeType := strings.ToUpper(strings.TrimSpace(createComputeType))
	if computeType == "" {
		computeType = "GPU"
	}
	if computeType != "GPU" && computeType != "CPU" {
		return fmt.Errorf("invalid --compute-type %q (use GPU or CPU)", createComputeType)
	}

	gpuTypeID := strings.TrimSpace(createGpuTypeID)
	if strings.Contains(gpuTypeID, ",") {
		return fmt.Errorf("only one gpu id is supported; use --gpu-count for multiple gpus of the same type")
	}
	if computeType == "CPU" && gpuTypeID != "" {
		return fmt.Errorf("--gpu-id must be empty when --compute-type is CPU")
	}
	if computeType == "GPU" && strings.TrimSpace(createInstanceID) != "" {
		return fmt.Errorf("--instance-id is only supported with --compute-type CPU")
	}

	if len(createModelReferences) > 0 && computeType != "GPU" {
		return fmt.Errorf("--model-reference is only supported with --compute-type GPU")
	}

	if createNetworkVolumeID != "" && createNetworkVolumeIDs != "" {
		return fmt.Errorf("--network-volume-id and --network-volume-ids are mutually exclusive")
	}

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	input := &api.EndpointCreateGQLInput{
		WorkersMin: createWorkersMin,
		WorkersMax: createWorkersMax,
	}

	// compute type: gpu uses a gpu pool id, cpu uses an instance id.
	if computeType == "CPU" {
		instanceID := strings.TrimSpace(createInstanceID)
		if instanceID == "" {
			instanceID = defaultCPUInstanceID
		}
		if !isCPUInstanceID(instanceID) {
			return fmt.Errorf("invalid --instance-id %q; expected a cpu flavor id like cpu3g-4-16", instanceID)
		}
		input.InstanceIDs = []string{instanceID}
		// gpu-only flags have no effect on cpu; tell the user instead of dropping silently.
		if createMinCudaVersion != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --min-cuda-version has no effect with --compute-type cpu; ignoring")
		}
		if cmd.Flags().Changed("gpu-count") {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --gpu-count has no effect with --compute-type cpu; ignoring")
		}
	} else {
		input.GpuCount = createGpuCount
		if gpuTypeID != "" {
			// saveEndpoint wants a gpu pool id, not a gpu type id; translate.
			poolID, err := client.ResolveServerlessGpuPoolID(gpuTypeID)
			if err != nil {
				return err
			}
			input.GpuIDs = poolID
		}
		if createMinCudaVersion != "" {
			input.MinCudaVersion = createMinCudaVersion
		}
	}

	if createScaleBy != "" {
		switch strings.ToLower(strings.TrimSpace(createScaleBy)) {
		case "delay":
			input.ScalerType = "QUEUE_DELAY"
		case "requests":
			input.ScalerType = "REQUEST_COUNT"
		default:
			return fmt.Errorf("invalid --scale-by %q (use delay or requests)", createScaleBy)
		}
	}

	if createScaleThreshold >= 0 {
		input.ScalerValue = createScaleThreshold
	}

	if createIdleTimeout >= 0 {
		if createIdleTimeout < 1 || createIdleTimeout > 3600 {
			return fmt.Errorf("--idle-timeout must be between 1 and 3600 seconds")
		}
		input.IdleTimeout = createIdleTimeout
	}

	if createExecutionTimeout >= 0 {
		// 0 is allowed (server treats it as "no per-request limit").
		input.ExecutionTimeoutMs = createExecutionTimeout * 1000
	}

	// flash boot maps to the flashBootType enum (off|flashboot); always set so
	// --flash-boot=false is honored (rest required a follow-up patch for this).
	if createFlashBoot {
		input.FlashBootType = "FLASHBOOT"
	} else {
		input.FlashBootType = "OFF"
	}

	if createDataCenterIDs != "" {
		input.Locations = createDataCenterIDs
	}

	if createNetworkVolumeID != "" {
		input.NetworkVolumeID = createNetworkVolumeID
	}
	if createNetworkVolumeIDs != "" {
		for _, id := range strings.Split(createNetworkVolumeIDs, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				input.NetworkVolumeIDs = append(input.NetworkVolumeIDs, api.NetworkVolumeIDInput{NetworkVolumeID: id})
			}
		}
	}

	if len(createModelReferences) > 0 {
		input.ModelReferences = createModelReferences
	}

	endpointName := createName

	// hub-id path: resolve listing, attach release + inline template (same as web ui).
	if createHubID != "" {
		listing, err := client.GetListing(createHubID)
		if err != nil {
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

		if endpointName == "" {
			endpointName = listing.Title
		}

		//nolint:gosec
		templateName := fmt.Sprintf("%s__template__%s", endpointName, randomString(7))

		input.HubReleaseID = release.ID
		input.Template = &api.EndpointTemplateInput{
			Name:              templateName,
			ImageName:         imageName,
			ContainerDiskInGb: containerDisk,
			DockerArgs:        "",
			Env:               envVars,
		}

		// fall back to the hub release's gpu pool ids when none were provided.
		// route through the resolver too, in case the hub config stores gpu type
		// ids rather than pool ids (pool ids pass through unchanged).
		if computeType == "GPU" && input.GpuIDs == "" && hubConfig.GpuIDs != "" {
			poolID, err := client.ResolveServerlessGpuPoolID(hubConfig.GpuIDs)
			if err != nil {
				return err
			}
			input.GpuIDs = poolID
		}
	} else {
		input.TemplateID = createTemplateID
		// --env only feeds an inline template (hub path); a referenced template's
		// env is fixed, so saveEndpoint ignores it. don't drop it silently.
		if len(createEnvVars) > 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --env has no effect with --template-id (env is defined by the template); ignoring")
		}
	}

	// saveEndpoint requires a name (min 3 chars); generate one when not given.
	if endpointName == "" {
		//nolint:gosec
		endpointName = fmt.Sprintf("endpoint-%s", randomString(8))
	}
	input.Name = endpointName

	endpoint, err := client.CreateEndpointGQL(input)
	if err != nil {
		return fmt.Errorf("failed to create endpoint: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}

// randomString builds a short lowercase suffix for generated endpoint/template
// names. it's display-only uniqueness, not a secret, so math/rand/v2 is fine.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range n {
		b[i] = letters[rand.IntN(len(letters))]
	}
	return string(b)
}

// isCPUInstanceID does a light client-side sanity check on a cpu flavor id so an
// obvious typo gives a clear error instead of an opaque graphql one. cpu flavor
// ids look like "<flavor>-<vcpu>-<ram>", e.g. cpu3g-4-16.
func isCPUInstanceID(id string) bool {
	return strings.HasPrefix(id, "cpu") && strings.Count(id, "-") >= 2
}
