package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/shlex"
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/duration"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/waitfor"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create a new pod",
	Long: `create a new pod.

you can create a pod either from a template or by specifying an image directly.

examples:
  # create from template (recommended)
  runpodctl pod create --template-id runpod-torch-v21 --gpu-id "NVIDIA GeForce RTX 4090"

  # create with custom image
  runpodctl pod create --image runpod/pytorch:1.0.3-cu1281-torch291-ubuntu2404 --gpu-id "NVIDIA GeForce RTX 4090"

  # create a cpu pod
  runpodctl pod create --compute-type cpu --image ubuntu:22.04

  # block until the pod's ssh is actually reachable, then print it
  runpodctl pod create --image runpod/pytorch:1.0.3-cu1281-torch291-ubuntu2404 --gpu-id "NVIDIA GeForce RTX 4090" --wait

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
	createNetworkVolumeID   string
	createMinCudaVersion    string
	createDockerArgs        string
	createRegistryAuthID    string
	createCountryCode       string
	createStopAfter         string
	createTerminateAfter    string
	createCompliance        string
	createWait              bool
	createWaitTimeout       string
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
	createCmd.Flags().StringVar(&createNetworkVolumeID, "network-volume-id", "", "network volume id to attach")
	createCmd.Flags().StringVar(&createMinCudaVersion, "min-cuda-version", "", "minimum cuda version (e.g., 12.6)")
	createCmd.Flags().StringVar(&createDockerArgs, "docker-args", "", "docker cmd arguments")
	createCmd.Flags().StringVar(&createRegistryAuthID, "registry-auth-id", "", "container registry auth id (from 'runpodctl registry list')")
	createCmd.Flags().StringVar(&createCountryCode, "country-code", "", "limit pod to a specific country (e.g., US, DE)")
	createCmd.Flags().StringVar(&createStopAfter, "stop-after", "", "auto-stop datetime (e.g., 2026-04-15T00:00:00Z)")
	createCmd.Flags().StringVar(&createTerminateAfter, "terminate-after", "", "auto-terminate datetime (e.g., 2026-04-15T00:00:00Z)")
	createCmd.Flags().StringVar(&createCompliance, "compliance", "", "comma-separated compliance requirements (e.g., HIPAA,SOC_2_TYPE_2)")
	createCmd.Flags().BoolVar(&createWait, "wait", false, "block until ssh is reachable (tcp connect to the pod's public port 22 answers with an ssh banner; no key or handshake needed), then print the pod as 'pod get' does. needs a publicly mapped port 22, so community cloud also needs --public-ip")
	createCmd.Flags().StringVar(&createWaitTimeout, "wait-timeout", defaultWaitTimeout, "max time to wait with --wait, e.g. 90s, 10m, 1h; on timeout the pod is kept and the error carries its id")
}

// defaultWaitTimeout is the --wait-timeout default. 10 minutes covers an image
// pull plus boot on a cold machine; a create that is not usable by then almost
// always needs a human, not more waiting.
const defaultWaitTimeout = "10m"

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
			// the hint has to live in the message: this returns before the
			// --data-center-ids note below, and runtime errors no longer print the
			// usage text that used to carry the flag name incidentally.
			return fmt.Errorf("global networking is only supported on secure cloud (set --cloud-type SECURE); if you passed --data-center-ids, they must be secure data centers")
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

	waitTimeout, err := resolveWaitTimeout(cmd, computeType, cloudType, supportPublicIP)
	if err != nil {
		return err
	}

	var result interface{}

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
		return fmt.Errorf("failed to create pod: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())

	if createWait {
		podID, idErr := podIDFrom(result)
		if idErr != nil {
			// the pod exists but we cannot address it; say so rather than waiting.
			return idErr
		}
		addr, waitErr := waitForPodSSH(cmd, podID, waitTimeout)
		if waitErr != nil {
			return waitErr
		}
		// re-read: the create response (either shape) has no live ssh info, and
		// handing back a pod you can connect to is the entire point of --wait.
		details, detailsErr := podDetailsWithSSH(podID, addr)
		if detailsErr != nil {
			return detailsErr
		}
		return output.Print(details, &output.Config{Format: format})
	}

	return output.Print(result, &output.Config{Format: format})
}

// resolveWaitTimeout validates the --wait flag combination and returns the
// timeout to use. It runs before the pod is created so an unsatisfiable
// combination costs nothing.
func resolveWaitTimeout(cmd *cobra.Command, computeType, cloudType string, supportPublicIP bool) (time.Duration, error) {
	if !createWait {
		if cmd.Flags().Changed("wait-timeout") {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: --wait-timeout has no effect without --wait; ignoring")
		}
		return 0, nil
	}

	if !createSSH {
		return 0, fmt.Errorf("--wait waits for ssh, so it cannot be combined with --ssh=false")
	}

	timeout, err := duration.Parse(createWaitTimeout)
	if err != nil {
		return 0, fmt.Errorf("invalid --wait-timeout: %w", err)
	}

	if computeType == "CPU" {
		// cpu pods are created over rest, which rejects startSsh, so runpod does
		// not set ssh up for them. prod does still allocate a public port 22, and
		// an image that runs its own sshd is reachable there — but plain images
		// are not, and that only shows up as a timeout. warn instead of guessing.
		fmt.Fprintln(cmd.ErrOrStderr(), "note: cpu pods are created through the rest api, which cannot request runpod-managed ssh; --wait only succeeds if the image starts sshd itself")
	}

	if cloudType == "COMMUNITY" && !supportPublicIP {
		// ssh readiness needs port 22 mapped to a public ip. on community cloud
		// that only happens on a machine with a public ip, which is what
		// --public-ip asks the scheduler for; without it the pod can land
		// somewhere that never publishes a port and the wait can only time out.
		// a warning rather than a refusal: the pod may still land on a machine
		// that does publish one, and refusing would remove a working combination.
		fmt.Fprintln(cmd.ErrOrStderr(), "note: community cloud only maps a public ssh port on machines with a public ip; add --public-ip (or use --cloud-type SECURE) or --wait may never see one")
	}

	return timeout, nil
}

// injection points for the wait, so its tests neither sleep nor hit the network.
var (
	newPodWaitLister    = func() (waitfor.PodLister, error) { return api.NewGraphQLClient() }
	podSSHProbe         waitfor.Prober // nil means waitfor.ProbeSSH
	waitPollInterval    = waitfor.DefaultInterval
	notifyWaitSignals   = signal.NotifyContext
	fetchPodDetailsFn   = fetchPodDetails
	postWaitReadTries   = 3
	postWaitReadBackoff = 2 * time.Second
)

// waitForPodSSH blocks until the pod's ssh is reachable and returns the address
// that answered. Progress goes to stderr; stdout stays a single json object.
func waitForPodSSH(cmd *cobra.Command, podID string, timeout time.Duration) (string, error) {
	lister, err := newPodWaitLister()
	if err != nil {
		return "", err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	// ctrl-c cancels the wait but must not lose the pod: the error below carries
	// its id and the delete command, because the pod bills either way.
	ctx, stop := notifyWaitSignals(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var addr string
	if _, err := waitfor.Until(ctx, waitfor.PodSSHPoller(lister, podID, podSSHProbe, &addr), waitfor.Options{
		Label:    "ssh on pod " + podID,
		Timeout:  timeout,
		Interval: waitPollInterval,
		Progress: cmd.ErrOrStderr(),
	}); err != nil {
		// The id goes into the error object as data, not only into the prose: a
		// caller must not have to regex a message to avoid leaking a billed pod.
		// The wording stops short of asserting the pod is still running, because
		// the wait may have ended precisely because it was terminated out of band.
		return "", output.WithResourceID(podID, fmt.Errorf("%w; pod %s was created: 'runpodctl pod get %s' to inspect it, 'runpodctl pod delete %s' if it is still running (pods bill by the second)", err, podID, podID, podID))
	}

	return addr, nil
}

// podDetailsWithSSH re-reads the pod once ssh is up, retrying while the read
// comes back without live ssh info.
//
// fetchPodDetails degrades to an {"error": ...} ssh blob when its graphql read
// fails. For `pod get` that best-effort answer is right, but for
// `pod create --wait` it would mean exiting 0 with the one field the flag exists
// to produce missing — a caller reading .ssh.ssh_command would get nothing and no
// signal. The failure is transient by nature (the wait just read that same list
// successfully), so retry, and fail loudly rather than quietly if it persists.
func podDetailsWithSSH(podID, addr string) (*podDetails, error) {
	var reason error
	for attempt := 1; attempt <= postWaitReadTries; attempt++ {
		if attempt > 1 {
			time.Sleep(postWaitReadBackoff)
		}
		// includeMachine: the graphql create response this replaces selected
		// machine { gpuDisplayName location }, so without it --wait would hand back
		// strictly less than a plain create did.
		details, err := fetchPodDetailsFn(podID, true, false)
		switch {
		case err != nil:
			reason = err
		case sshInfoMissing(details):
			reason = fmt.Errorf("the pod read back without live ssh info: %s", sshInfoError(details))
		default:
			return details, nil
		}
	}

	return nil, output.WithResourceID(podID, fmt.Errorf("pod %s is running and ssh answered at %s, but reading the pod back failed: %w; retry with 'runpodctl pod get %s'", podID, addrOrUnknown(addr), reason, podID))
}

func addrOrUnknown(addr string) string {
	if addr == "" {
		return "its public ssh port"
	}
	return addr
}

// podIDFrom pulls the pod id out of either create response shape: the rest path
// returns *api.Pod, the graphql path an untyped map.
func podIDFrom(result interface{}) (string, error) {
	switch typed := result.(type) {
	case *api.Pod:
		if typed != nil && typed.ID != "" {
			return typed.ID, nil
		}
	case map[string]interface{}:
		if id, ok := typed["id"].(string); ok && id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("pod was created but the response carried no id, so --wait cannot poll it; find it with 'runpodctl pod list'")
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

	if createNetworkVolumeID != "" {
		req.NetworkVolumeId = createNetworkVolumeID
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

	if createMinCudaVersion != "" {
		req.MinCudaVersion = createMinCudaVersion
	}

	if createDockerArgs != "" {
		req.DockerArgs = createDockerArgs
	}

	if trimmed := strings.TrimSpace(createRegistryAuthID); trimmed != "" {
		req.ContainerRegistryAuthId = trimmed
	}

	if createCountryCode != "" {
		req.CountryCode = createCountryCode
	}

	if createStopAfter != "" {
		req.StopAfter = createStopAfter
	}

	if createTerminateAfter != "" {
		req.TerminateAfter = createTerminateAfter
	}

	if createCompliance != "" {
		req.Compliance = strings.Split(createCompliance, ",")
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

	if createNetworkVolumeID != "" {
		req.NetworkVolumeID = createNetworkVolumeID
	}

	if createPorts != "" {
		req.Ports = strings.Split(createPorts, ",")
	}

	if createDataCenterIDs != "" {
		req.DataCenterIDs = strings.Split(createDataCenterIDs, ",")
	}

	if createMinCudaVersion != "" {
		req.MinCudaVersion = createMinCudaVersion
	}

	if createDockerArgs != "" {
		req.DockerStartCmd, req.DockerEntrypoint = parseDockerArgs(createDockerArgs)
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

// parseDockerArgs converts the --docker-args string into the dockerStartCmd /
// dockerEntrypoint arrays the REST API expects (its schema has no dockerArgs
// field and rejects it as an extra key). It mirrors the backend's decoding of
// legacy dockerArgs strings so the flag means the same thing on the GraphQL
// and REST paths: a JSON `{"cmd":[...],"entrypoint":[...]}` object (the
// backend's canonical encoding, also produced by template create) is used
// as-is, anything else is shlex-split into the start cmd, falling back to a
// whitespace split when the shell lexer fails (e.g. unbalanced quotes).
func parseDockerArgs(args string) (cmd, entrypoint []string) {
	var parsed struct {
		Cmd        []string `json:"cmd"`
		Entrypoint []string `json:"entrypoint"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err == nil {
		return parsed.Cmd, parsed.Entrypoint
	}
	tokens, err := shlex.Split(args)
	if err != nil {
		return strings.Fields(args), nil
	}
	return tokens, nil
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
