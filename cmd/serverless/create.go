package serverless

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/duration"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/waitfor"

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
hub deployment constraints are applied unless explicitly overridden by cli flags.

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

  # block until a worker is ready (--workers-min 1 starts one now, and bills for it)
  runpodctl serverless create --template-id <id> --gpu-id "NVIDIA GeForce RTX 4090" --workers-min 1 --wait

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
	createWait             bool
	createWaitTimeout      string
)

// defaultWaitTimeout is the --wait-timeout default; a cold worker has to pull
// the image and start the handler, which routinely takes minutes.
const defaultWaitTimeout = "10m"

// waitPollInterval is how often --wait polls the endpoint's health. It is a var
// only so the tests can shorten it instead of sleeping between fake polls.
var waitPollInterval = waitfor.DefaultInterval

type serverlessCreateClient interface {
	GetListing(string) (*api.Listing, error)
	ResolveServerlessGpuPoolID(string) (string, error)
	CreateEndpointGQL(*api.EndpointCreateGQLInput) (*api.Endpoint, error)
}

var newServerlessCreateClient = func() (serverlessCreateClient, error) {
	return api.NewClient()
}

// --wait reads /health, which is the invoke service rather than the control
// plane, so it needs the invoke client -- the same one `serverless health` and
// `serverless run` use. It is built only when --wait is set, so a plain create
// still needs nothing from that service.
var newWaitHealthClient = func() (waitfor.EndpointHealthGetter, error) {
	client, err := api.NewInvokeClient()
	if err != nil {
		return nil, err
	}
	return boundedHealthClient{client: client}, nil
}

// boundedHealthClient bounds each /health read by the shared "timeout" config
// key.
//
// The invoke client deliberately has no client-wide timeout -- every call carries
// its own deadline -- and the wait loop's context has none either, since the loop
// owns the overall budget itself. Without this a single wedged read would hang the
// whole wait instead of counting as one failed poll, which is the opposite of the
// "a transient failure is just an unknown state" rule the wait is built on.
type boundedHealthClient struct {
	client *api.InvokeClient
}

func (b boundedHealthClient) EndpointHealthCounts(ctx context.Context, endpointID string) (*api.EndpointHealth, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout())
	defer cancel()
	return b.client.EndpointHealthCounts(ctx, endpointID)
}

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
	createCmd.Flags().BoolVar(&createWait, "wait", false, "block until the endpoint's health reports a worker ready or running. a ready worker may be flashboot-cached, so the first request still resumes it; --workers-min 1 is the fastest way to get one")
	createCmd.Flags().StringVar(&createWaitTimeout, "wait-timeout", defaultWaitTimeout, "max time to wait with --wait, e.g. 90s, 10m, 1h; on timeout the endpoint is kept and the error carries its id")
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
	if createHubID == "" || flagChanged(cmd, "compute-type") {
		// Validate before the Hub lookup when its compute type cannot override it.
		if err := validateComputeFlags(computeType, gpuTypeID, createInstanceID, createModelReferences); err != nil {
			return err
		}
	}

	if createNetworkVolumeID != "" && createNetworkVolumeIDs != "" {
		return fmt.Errorf("--network-volume-id and --network-volume-ids are mutually exclusive")
	}

	waitTimeout, err := resolveWaitTimeout(cmd)
	if err != nil {
		return err
	}

	client, err := newServerlessCreateClient()
	if err != nil {
		return err
	}

	var listing *api.Listing
	var release *api.HubRelease
	var hubConfig api.HubReleaseConfig
	var imageName string
	if createHubID != "" {
		listing, err = client.GetListing(createHubID)
		if err != nil {
			return fmt.Errorf("failed to get hub listing: %w", err)
		}
		if listing.ListedRelease == nil {
			return fmt.Errorf("hub listing %q has no published release", createHubID)
		}

		release = listing.ListedRelease
		if release.Build != nil {
			imageName = release.Build.ImageName
		}
		if imageName == "" {
			return fmt.Errorf("hub listing %q has no built image; the release may still be building", createHubID)
		}

		if release.Config != "" {
			if err := json.Unmarshal([]byte(release.Config), &hubConfig); err != nil {
				return fmt.Errorf("failed to parse hub release config for %q: %w", createHubID, err)
			}
		}
	}

	if createHubID != "" && !flagChanged(cmd, "compute-type") && hubConfig.RunsOn != "" {
		computeType = strings.ToUpper(strings.TrimSpace(hubConfig.RunsOn))
	}
	if computeType != "GPU" && computeType != "CPU" {
		return fmt.Errorf("hub listing %q has unsupported runsOn value %q", createHubID, hubConfig.RunsOn)
	}
	if err := validateComputeFlags(computeType, gpuTypeID, createInstanceID, createModelReferences); err != nil {
		if createHubID != "" && !flagChanged(cmd, "compute-type") && hubConfig.RunsOn != "" {
			return fmt.Errorf("hub listing %q requires %s: %w", createHubID, computeType, err)
		}
		return err
	}

	input := &api.EndpointCreateGQLInput{
		WorkersMin: &createWorkersMin,
		WorkersMax: &createWorkersMax,
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
		if flagChanged(cmd, "gpu-count") {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --gpu-count has no effect with --compute-type cpu; ignoring")
		}
	} else {
		input.GpuCount = hubGPUCount(hubConfig, createGpuCount, flagChanged(cmd, "gpu-count"))
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
		} else if createHubID != "" {
			input.MinCudaVersion = hubMinCudaVersion(hubConfig.AllowedCudaVersions)
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
		ms := createExecutionTimeout * 1000
		input.ExecutionTimeoutMs = &ms
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

	// hub-id path: attach the resolved release and an inline template (same as web ui).
	if createHubID != "" {
		containerDisk := 10
		if hubConfig.ContainerDiskInGb > 0 {
			containerDisk = hubConfig.ContainerDiskInGb
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

	if createWait {
		healthClient, healthErr := newWaitHealthClient()
		if healthErr != nil {
			// the endpoint exists at this point, so say so rather than losing it.
			return output.WithResourceID(endpoint.ID, fmt.Errorf("endpoint %s was created, but --wait cannot read its health: %w", endpoint.ID, healthErr))
		}
		if err := waitForReadyWorker(cmd, healthClient, endpoint.ID, waitTimeout); err != nil {
			return err
		}
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}

// resolveWaitTimeout validates the --wait flag combination and returns the
// timeout to use. It runs before the endpoint is created, so a combination that
// can never be satisfied costs nothing.
func resolveWaitTimeout(cmd *cobra.Command) (time.Duration, error) {
	if !createWait {
		if flagChanged(cmd, "wait-timeout") {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --wait-timeout has no effect without --wait; ignoring")
		}
		return 0, nil
	}

	timeout, err := duration.Parse(createWaitTimeout)
	if err != nil {
		return 0, fmt.Errorf("invalid --wait-timeout: %w", err)
	}

	// --workers-min 0 is satisfiable, so it warns rather than refusing: ai-api
	// floors workersStandby to 5 whenever workersMax > 1 regardless of workersMin
	// (pkg/graphql/aiapi.go finalEndpoint), worker.Sync then launches cache
	// workers to fill standby (pkg/worker/sync.go), and /health counts a cached
	// worker as ready (pkg/api/health.go). Verified live: six endpoints on a prod
	// account, all with workersMin unset, report ready 1-5 with running 0.
	// It is still slower and less certain than an explicit warm worker, hence the
	// note; refusing would break the flag's most common invocation.
	if createWorkersMin < 1 {
		fmt.Fprintln(cmd.ErrOrStderr(), "note: at --workers-min 0 no worker is guaranteed to start; runpod only fills a standby pool when --workers-max is above 1, so --wait may take longer or time out. --workers-min 1 starts (and bills) a worker immediately")
	}

	return timeout, nil
}

// notifyWaitSignals is signal.NotifyContext, injectable so a test can prove the
// wait actually registers for ctrl-c.
var notifyWaitSignals = signal.NotifyContext

// waitForReadyWorker blocks until the endpoint's /health reports a worker ready
// or running (see waitfor.EndpointWorkerPoller for what each counter actually
// proves). Progress goes to stderr so stdout stays a single json object.
func waitForReadyWorker(cmd *cobra.Command, client waitfor.EndpointHealthGetter, endpointID string, timeout time.Duration) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	// ctrl-c stops the wait but must not lose the endpoint: the error names it.
	// SignalContext releases the handler on the first signal so a second ctrl-c is
	// not swallowed if a read is slow to notice the cancellation.
	ctx, stop := waitfor.SignalContext(ctx, notifyWaitSignals, os.Interrupt, syscall.SIGTERM)
	defer stop()

	_, err := waitfor.Until(ctx, waitfor.EndpointWorkerPoller(client, endpointID), waitfor.Options{
		Label:    "a ready worker on endpoint " + endpointID,
		Timeout:  timeout,
		Interval: waitPollInterval,
		Progress: cmd.ErrOrStderr(),
	})
	if err != nil {
		// the id goes into the error object as data, not only into the prose: a
		// caller must not have to regex a message to find the endpoint it now owns.
		return output.WithResourceID(endpointID, fmt.Errorf("%w; endpoint %s was created: 'runpodctl serverless get %s' to inspect it, 'runpodctl serverless delete %s' if you no longer want it (an endpoint with no running worker is not billing, but it will start one on the first request)", err, endpointID, endpointID, endpointID))
	}
	return nil
}

// flagChanged reports whether a command-line value was explicitly provided.
// Hub release values must override CLI defaults, but never explicit user input.
func flagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}

func hubGPUCount(config api.HubReleaseConfig, cliValue int, cliChanged bool) int {
	if !cliChanged && config.GpuCount > 0 {
		return config.GpuCount
	}
	return cliValue
}

// hubMinCudaVersion translates the ordered Hub constraint list to EndpointInput's
// single minCudaVersion field. The first non-empty Hub value is sent; blank
// optional values are ignored.
func hubMinCudaVersion(versions []string) string {
	for _, version := range versions {
		if version = strings.TrimSpace(version); version != "" {
			return version
		}
	}
	return ""
}

func validateComputeFlags(computeType, gpuTypeID, instanceID string, modelReferences []string) error {
	if computeType == "CPU" && gpuTypeID != "" {
		return fmt.Errorf("--gpu-id must be empty when --compute-type is CPU")
	}
	if computeType == "GPU" && strings.TrimSpace(instanceID) != "" {
		return fmt.Errorf("--instance-id is only supported with --compute-type CPU")
	}
	if len(modelReferences) > 0 && computeType != "GPU" {
		return fmt.Errorf("--model-reference is only supported with --compute-type GPU")
	}
	return nil
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
