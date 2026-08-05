package serverless

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/runpod/runpodctl/internal/waitfor"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestServerlessCmd_Structure(t *testing.T) {
	if Cmd.Use != "serverless" {
		t.Errorf("expected use 'serverless', got %s", Cmd.Use)
	}

	// check alias is only sls
	if len(Cmd.Aliases) != 1 {
		t.Errorf("expected exactly 1 alias, got %d", len(Cmd.Aliases))
	}
	if Cmd.Aliases[0] != "sls" {
		t.Errorf("expected alias 'sls', got %s", Cmd.Aliases[0])
	}

	// check subcommands exist
	expectedSubcommands := []string{
		"list", "get <endpoint-id>", "create", "update <endpoint-id>", "delete <endpoint-id>",
		"health <endpoint-id>", "run <endpoint-id>", "status <endpoint-id> <job-id>",
	}
	for _, expected := range expectedSubcommands {
		found := false
		for _, cmd := range Cmd.Commands() {
			if cmd.Use == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %s not found", expected)
		}
	}
}

func TestListCmd_Flags(t *testing.T) {
	flags := listCmd.Flags()

	if flags.Lookup("include-template") == nil {
		t.Error("expected --include-template flag")
	}
	if flags.Lookup("include-workers") == nil {
		t.Error("expected --include-workers flag")
	}
}

func TestCreateCmd_Flags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("template-id") == nil {
		t.Error("expected --template-id flag")
	}
	if flags.Lookup("gpu-id") == nil {
		t.Error("expected --gpu-id flag")
	}
	if flags.Lookup("workers-min") == nil {
		t.Error("expected --workers-min flag")
	}
	if flags.Lookup("workers-max") == nil {
		t.Error("expected --workers-max flag")
	}
	if flags.Lookup("model-reference") == nil {
		t.Error("expected --model-reference flag")
	}
	if flags.Lookup("compute-type") == nil {
		t.Error("expected --compute-type flag")
	}
	if flags.Lookup("instance-id") == nil {
		t.Error("expected --instance-id flag")
	}
	if flags.Lookup("network-volume-ids") == nil {
		t.Error("expected --network-volume-ids flag")
	}
}

// snapshotCreateFlags restores every serverless-create global after a test that
// mutates them, so package-level state doesn't leak between tests, then sets a
// known-good baseline. individual tests override only what they exercise.
func snapshotCreateFlags(t *testing.T) {
	t.Helper()
	old := struct {
		name, templateID, hubID, computeType, gpuID, instanceID string
		dataCenterIDs, networkVolumeID, networkVolumeIDs        string
		minCudaVersion, scaleBy, waitTimeout                    string
		gpuCount, workersMin, workersMax                        int
		scaleThreshold, idleTimeout, executionTimeout           int
		flashBoot, wait                                         bool
		envVars, modelReferences                                []string
	}{
		createName, createTemplateID, createHubID, createComputeType, createGpuTypeID, createInstanceID,
		createDataCenterIDs, createNetworkVolumeID, createNetworkVolumeIDs,
		createMinCudaVersion, createScaleBy, createWaitTimeout,
		createGpuCount, createWorkersMin, createWorkersMax,
		createScaleThreshold, createIdleTimeout, createExecutionTimeout,
		createFlashBoot, createWait,
		createEnvVars, createModelReferences,
	}
	t.Cleanup(func() {
		createName, createTemplateID, createHubID = old.name, old.templateID, old.hubID
		createComputeType, createGpuTypeID, createInstanceID = old.computeType, old.gpuID, old.instanceID
		createDataCenterIDs, createNetworkVolumeID, createNetworkVolumeIDs = old.dataCenterIDs, old.networkVolumeID, old.networkVolumeIDs
		createMinCudaVersion, createScaleBy = old.minCudaVersion, old.scaleBy
		createGpuCount, createWorkersMin, createWorkersMax = old.gpuCount, old.workersMin, old.workersMax
		createScaleThreshold, createIdleTimeout, createExecutionTimeout = old.scaleThreshold, old.idleTimeout, old.executionTimeout
		createFlashBoot, createWait = old.flashBoot, old.wait
		createWaitTimeout = old.waitTimeout
		createEnvVars, createModelReferences = old.envVars, old.modelReferences
	})
	// known-good baseline matching the flag defaults; tests override per case.
	createName, createTemplateID, createHubID = "", "tpl-123", ""
	createComputeType, createGpuTypeID, createInstanceID = "GPU", "", ""
	createDataCenterIDs, createNetworkVolumeID, createNetworkVolumeIDs = "", "", ""
	createMinCudaVersion, createScaleBy = "", ""
	createGpuCount, createWorkersMin, createWorkersMax = 1, 0, 3
	createScaleThreshold, createIdleTimeout, createExecutionTimeout = -1, -1, -1
	createFlashBoot = true
	createWait, createWaitTimeout = false, defaultWaitTimeout
	createEnvVars, createModelReferences = nil, nil
}

// releaseObservingClient records, at the moment of a health read, whether the
// signal registration has already been torn down.
type releaseObservingClient struct {
	waitfor.EndpointHealthGetter
	released <-chan struct{}
	observed *bool
}

func (c *releaseObservingClient) EndpointHealthCounts(ctx context.Context, id string) (*api.EndpointHealth, error) {
	select {
	case <-c.released:
		*c.observed = true
	case <-time.After(2 * time.Second):
	}
	return c.EndpointHealthGetter.EndpointHealthCounts(ctx, id)
}

// installMockWaitHealthClient points --wait's health reads at the same mock. The
// wait uses the invoke client, not the control-plane client, because /health is a
// different service -- so a test that exercises --wait has to install both.
func installMockWaitHealthClient(t *testing.T, client waitfor.EndpointHealthGetter) {
	t.Helper()
	old := newWaitHealthClient
	t.Cleanup(func() { newWaitHealthClient = old })
	newWaitHealthClient = func() (waitfor.EndpointHealthGetter, error) { return client, nil }
}

type mockServerlessCreateClient struct {
	listing       *api.Listing
	getListingHit bool
	createInput   *api.EndpointCreateGQLInput
	// health is served to --wait; healthCalls counts the polls so a test can
	// prove the wait ran (or did not).
	health          []api.EndpointHealthWorkers
	healthCalls     int
	healthErr       error
	healthErrsFirst int
	healthErrFirst  error
}

func (c *mockServerlessCreateClient) GetListing(string) (*api.Listing, error) {
	c.getListingHit = true
	return c.listing, nil
}

func (c *mockServerlessCreateClient) ResolveServerlessGpuPoolID(gpuID string) (string, error) {
	return gpuID, nil
}

func (c *mockServerlessCreateClient) CreateEndpointGQL(input *api.EndpointCreateGQLInput) (*api.Endpoint, error) {
	c.createInput = input
	return &api.Endpoint{ID: "endpoint-1", Name: input.Name}, nil
}

func (c *mockServerlessCreateClient) EndpointHealthCounts(context.Context, string) (*api.EndpointHealth, error) {
	c.healthCalls++
	if c.healthErr != nil {
		return nil, c.healthErr
	}
	// healthErrsFirst fails the first n polls, standing in for the invoke service
	// not knowing the endpoint id yet.
	if c.healthCalls <= c.healthErrsFirst {
		return nil, c.healthErrFirst
	}
	idx := c.healthCalls - 1
	if idx >= len(c.health) {
		if len(c.health) == 0 {
			return &api.EndpointHealth{}, nil
		}
		idx = len(c.health) - 1
	}
	return &api.EndpointHealth{Workers: c.health[idx]}, nil
}

func mockCreateCommand(changedFlags ...string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("compute-type", "GPU", "")
	cmd.Flags().Int("gpu-count", 1, "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("wait-timeout", defaultWaitTimeout, "")
	for _, name := range changedFlags {
		cmd.Flags().Lookup(name).Changed = true
	}
	return cmd
}

func mockHubListing(config string) *api.Listing {
	return &api.Listing{
		ID:    "hub-1",
		Title: "Hub endpoint",
		ListedRelease: &api.HubRelease{
			ID:     "release-1",
			Config: config,
			Build:  &api.GitBuild{ImageName: "runpod/test:latest"},
		},
	}
}

// these validations all run before any api client/network call, so they're
// safe to exercise without hitting the live api.
func TestCreateCmd_Validations(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name:    "invalid compute type",
			setup:   func() { createComputeType = "TPU" },
			wantErr: "invalid --compute-type",
		},
		{
			name:    "cpu with gpu-id",
			setup:   func() { createComputeType = "CPU"; createGpuTypeID = "NVIDIA A40" },
			wantErr: "--gpu-id must be empty when --compute-type is CPU",
		},
		{
			name:    "gpu with instance-id",
			setup:   func() { createComputeType = "GPU"; createInstanceID = "cpu3g-4-16" },
			wantErr: "--instance-id is only supported with --compute-type CPU",
		},
		{
			name:    "both network volume flags",
			setup:   func() { createNetworkVolumeID = "vol-1"; createNetworkVolumeIDs = "vol-2,vol-3" },
			wantErr: "--network-volume-id and --network-volume-ids are mutually exclusive",
		},
		{
			name:    "cpu with model reference",
			setup:   func() { createComputeType = "CPU"; createModelReferences = []string{"https://x/y:z"} },
			wantErr: "--model-reference is only supported with --compute-type GPU",
		},
		{
			name:    "name too short",
			setup:   func() { createName = "ab" },
			wantErr: "--name must be at least 3 characters",
		},
		{
			name:    "scale-threshold below 1",
			setup:   func() { createScaleThreshold = 0 },
			wantErr: "--scale-threshold must be at least 1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotCreateFlags(t)
			tc.setup()
			err := runCreate(&cobra.Command{}, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestHubDeploymentConstraintPrecedence(t *testing.T) {
	config := api.HubReleaseConfig{
		GpuCount:            2,
		AllowedCudaVersions: []string{"13.0", "12.8"},
	}

	if got := hubGPUCount(config, 1, false); got != 2 {
		t.Fatalf("hub gpu count = %d, want 2", got)
	}
	if got := hubGPUCount(config, 4, true); got != 4 {
		t.Fatalf("explicit gpu count = %d, want 4", got)
	}
	if got := hubMinCudaVersion(config.AllowedCudaVersions); got != "13.0" {
		t.Fatalf("hub cuda version = %q, want 13.0", got)
	}
	if got := hubMinCudaVersion([]string{" ", "12.8"}); got != "12.8" {
		t.Fatalf("blank cuda values were not ignored: %q", got)
	}
}

func TestRunCreate_HubDeploymentConstraints(t *testing.T) {
	cases := []struct {
		name       string
		config     string
		setup      func()
		wantInput  func(*testing.T, *api.EndpointCreateGQLInput)
		wantErr    string
		wantCreate bool
	}{
		{
			name:   "applies gpu constraints",
			config: `{"runsOn":"GPU","gpuCount":2,"allowedCudaVersions":["13.0","12.8"]}`,
			wantInput: func(t *testing.T, input *api.EndpointCreateGQLInput) {
				t.Helper()
				if input.GpuCount != 2 || input.MinCudaVersion != "13.0" {
					t.Fatalf("gpu constraints = count %d, cuda %q", input.GpuCount, input.MinCudaVersion)
				}
			},
			wantCreate: true,
		},
		{
			name:   "runsOn overrides default compute type",
			config: `{"runsOn":"CPU"}`,
			wantInput: func(t *testing.T, input *api.EndpointCreateGQLInput) {
				t.Helper()
				if len(input.InstanceIDs) != 1 || input.InstanceIDs[0] != defaultCPUInstanceID {
					t.Fatalf("instance ids = %v, want default cpu instance", input.InstanceIDs)
				}
			},
			wantCreate: true,
		},
		{
			name:    "reports hub compute constraint",
			config:  `{"runsOn":"CPU"}`,
			setup:   func() { createGpuTypeID = "NVIDIA A40" },
			wantErr: `hub listing "hub-1" requires CPU: --gpu-id must be empty`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotCreateFlags(t)
			createTemplateID, createHubID = "", "hub-1"
			if tc.setup != nil {
				tc.setup()
			}

			client := &mockServerlessCreateClient{listing: mockHubListing(tc.config)}
			oldFactory := newServerlessCreateClient
			newServerlessCreateClient = func() (serverlessCreateClient, error) { return client, nil }
			t.Cleanup(func() { newServerlessCreateClient = oldFactory })
			installMockWaitHealthClient(t, client)

			err := runCreate(mockCreateCommand(), nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				if !client.getListingHit {
					t.Fatal("hub lookup was skipped before Hub-derived validation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantCreate && client.createInput == nil {
				t.Fatal("endpoint create was not called")
			}
			tc.wantInput(t, client.createInput)
		})
	}
}

func TestCreateCmd_WaitFlags(t *testing.T) {
	flags := createCmd.Flags()

	if flags.Lookup("wait") == nil {
		t.Error("expected --wait flag")
	}
	waitTimeout := flags.Lookup("wait-timeout")
	if waitTimeout == nil {
		t.Fatal("expected --wait-timeout flag")
	}
	if waitTimeout.DefValue != "10m" {
		t.Errorf("--wait-timeout default = %q, want 10m", waitTimeout.DefValue)
	}
}

// --wait at --workers-min 0 warns but still runs. ai-api floors workersStandby
// to 5 whenever workersMax > 1 (pkg/graphql/aiapi.go finalEndpoint), Sync then
// launches cache workers to fill it (pkg/worker/sync.go) and /health counts a
// cached worker as ready (pkg/api/health.go), so the wait is satisfiable — this
// used to be refused on the false premise that nothing is ever provisioned.
func TestRunCreate_WaitAtZeroMinWorkersWarnsAndStillWaits(t *testing.T) {
	snapshotCreateFlags(t)
	createWait = true
	createWorkersMin = 0
	createWaitTimeout = "1ms"
	oldInterval := waitPollInterval
	waitPollInterval = time.Millisecond
	t.Cleanup(func() { waitPollInterval = oldInterval })

	client := &mockServerlessCreateClient{}
	oldFactory := newServerlessCreateClient
	newServerlessCreateClient = func() (serverlessCreateClient, error) { return client, nil }
	t.Cleanup(func() { newServerlessCreateClient = oldFactory })
	installMockWaitHealthClient(t, client)

	cmd := mockCreateCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := runCreate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want a wait timeout (the wait must run, not be refused)", err)
	}
	if !strings.Contains(stderr.String(), "--workers-min 0") {
		t.Errorf("stderr = %q, want a note about --workers-min 0", stderr.String())
	}
	if client.createInput == nil {
		t.Error("the endpoint must still be created")
	}
	if client.healthCalls == 0 {
		t.Error("health must still be polled")
	}
}

func TestRunCreate_Wait(t *testing.T) {
	cases := []struct {
		name        string
		workersMin  int
		waitTimeout string
		health      []api.EndpointHealthWorkers
		wantPolls   int
		wantErr     string
		wantCode    string
		wantStderr  string
	}{
		{
			name:        "returns as soon as a worker is ready",
			workersMin:  1,
			waitTimeout: "10m",
			health: []api.EndpointHealthWorkers{
				{Initializing: 1},
				{Ready: 1, Idle: 1},
			},
			wantPolls: 2,
			// the success line has to name which counter satisfied the wait: ready and
			// running are both accepted, and running is true of a merely-scheduled
			// worker, so an unattributed "ready after" is not evidence of anything.
			wantStderr: "ready after 0s: workers ready 1, running 0",
		},
		{
			name:        "times out with the worker counts and the endpoint id",
			workersMin:  1,
			waitTimeout: "1ms",
			health:      []api.EndpointHealthWorkers{{Throttled: 1}},
			wantErr:     "endpoint endpoint-1 was created",
			wantCode:    waitfor.CodeTimeout,
			wantStderr:  "waiting for a ready worker on endpoint endpoint-1",
		},
		{
			name:        "rejects an unparseable timeout before creating anything",
			workersMin:  1,
			waitTimeout: "soon",
			wantErr:     `invalid --wait-timeout: invalid duration "soon"`,
		},
	}

	oldInterval := waitPollInterval
	waitPollInterval = time.Millisecond
	t.Cleanup(func() { waitPollInterval = oldInterval })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshotCreateFlags(t)
			createWait = true
			createWorkersMin = tc.workersMin
			createWaitTimeout = tc.waitTimeout

			client := &mockServerlessCreateClient{health: tc.health}
			oldFactory := newServerlessCreateClient
			newServerlessCreateClient = func() (serverlessCreateClient, error) { return client, nil }
			t.Cleanup(func() { newServerlessCreateClient = oldFactory })
			installMockWaitHealthClient(t, client)

			cmd := mockCreateCommand()
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)

			err := runCreate(cmd, nil)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if client.healthCalls != tc.wantPolls {
					t.Errorf("health polled %d times, want %d", client.healthCalls, tc.wantPolls)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				if tc.wantCode != "" {
					var waitErr *waitfor.Error
					if !errors.As(err, &waitErr) {
						t.Fatalf("expected a *waitfor.Error, got %#v", err)
					}
					if waitErr.ErrorCode() != tc.wantCode {
						t.Errorf("code = %q, want %q", waitErr.ErrorCode(), tc.wantCode)
					}
					if !strings.Contains(err.Error(), "throttled 1") {
						t.Errorf("the timeout error must carry the last known state: %v", err)
					}
				}
			}

			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.wantStderr)
			}
		})
	}
}

// The invoke service that serves /health is not the service that created the
// endpoint, and it 404s an id it has not learned yet (confirmed against prod).
// A wait that aborted on that first poll reported not_found for an endpoint that
// exists and is billing a warm worker — and not_found reads as "the create
// failed", so an agent would have created a second one.
func TestRunCreate_WaitSurvivesTransientHealthFailures(t *testing.T) {
	snapshotCreateFlags(t)
	createWait = true
	createWorkersMin = 1
	createWaitTimeout = "10m"

	oldInterval := waitPollInterval
	waitPollInterval = time.Millisecond
	t.Cleanup(func() { waitPollInterval = oldInterval })

	client := &mockServerlessCreateClient{
		health:          []api.EndpointHealthWorkers{{Ready: 1}},
		healthErrsFirst: 2,
		healthErrFirst:  &api.APIError{Message: "endpoint not found", Code: "not_found", Status: 404},
	}
	oldFactory := newServerlessCreateClient
	newServerlessCreateClient = func() (serverlessCreateClient, error) { return client, nil }
	t.Cleanup(func() { newServerlessCreateClient = oldFactory })
	installMockWaitHealthClient(t, client)

	cmd := mockCreateCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	if err := runCreate(cmd, nil); err != nil {
		t.Fatalf("a transient health failure must not end the wait: %v", err)
	}
	if client.healthCalls != 3 {
		t.Errorf("health polled %d times, want 3 (two failures then ready)", client.healthCalls)
	}
	if !strings.Contains(stderr.String(), "ready after") {
		t.Errorf("stderr = %q, want the readiness line", stderr.String())
	}
}

// Credentials will not fix themselves, so that one does end the wait rather than
// re-poll a doomed call for ten minutes.
func TestRunCreate_WaitAbortsOnUnauthorized(t *testing.T) {
	snapshotCreateFlags(t)
	createWait = true
	createWorkersMin = 1
	// a tiny budget as well as a huge interval: healthCalls == 1 still proves the
	// fail-fast, but a regression fails in 300ms instead of hanging on the 10m
	// default until `go test` panics.
	createWaitTimeout = "300ms"

	oldInterval := waitPollInterval
	waitPollInterval = time.Hour
	t.Cleanup(func() { waitPollInterval = oldInterval })

	client := &mockServerlessCreateClient{
		healthErr: &api.APIError{Message: "unauthorized", Code: "unauthorized", Status: 401},
	}
	oldFactory := newServerlessCreateClient
	newServerlessCreateClient = func() (serverlessCreateClient, error) { return client, nil }
	t.Cleanup(func() { newServerlessCreateClient = oldFactory })
	installMockWaitHealthClient(t, client)

	err := runCreate(mockCreateCommand(), nil)
	if err == nil {
		t.Fatal("expected the unauthorized error to end the wait")
	}
	if client.healthCalls != 1 {
		t.Errorf("health polled %d times, want 1", client.healthCalls)
	}
	// the endpoint still exists, so its id must be recoverable as data.
	if id := resourceIDOf(err); id != "endpoint-1" {
		t.Errorf("error id = %q, want endpoint-1", id)
	}
}

// resourceIDOf reads the machine-readable resource id off an error, the way
// internal/output does when it emits the json error object.
func resourceIDOf(err error) string {
	var ider interface{ ErrorResourceID() string }
	if errors.As(err, &ider) {
		return ider.ErrorResourceID()
	}
	return ""
}

// ctrl-c during a wait must not lose the endpoint, so the wait has to be
// registered for SIGINT/SIGTERM specifically.
func TestWaitForReadyWorkerHandlesInterrupts(t *testing.T) {
	oldNotify := notifyWaitSignals
	t.Cleanup(func() { notifyWaitSignals = oldNotify })

	var registered []os.Signal
	// released reports that the signal registration was torn down, which
	// waitfor.SignalContext does as soon as the first signal lands -- not when the
	// caller finishes. a plain notifyWaitSignals call would only release on the
	// deferred stop, i.e. after the wait had already returned.
	released := make(chan struct{})
	notifyWaitSignals = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		registered = signals
		ctx, cancel := context.WithCancel(parent)
		cancel() // stand in for the signal arriving during the wait
		return ctx, func() {
			cancel()
			select {
			case <-released:
			default:
				close(released)
			}
		}
	}

	// the health read stands in for "work still in flight when the signal lands":
	// it reports whether the registration was already released while the wait was
	// still running, which is the whole point of SignalContext.
	inner := &mockServerlessCreateClient{health: []api.EndpointHealthWorkers{{Initializing: 1}}}
	releasedDuringWait := false
	client := &releaseObservingClient{EndpointHealthGetter: inner, released: released, observed: &releasedDuringWait}
	cmd := mockCreateCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	err := waitForReadyWorker(cmd, client, "endpoint-1", time.Minute)
	if !releasedDuringWait {
		t.Error("the signal registration must be released while the wait is still running, or a second ctrl-c is swallowed until the in-flight api call gives up")
	}
	var waitErr *waitfor.Error
	if !errors.As(err, &waitErr) {
		t.Fatalf("expected a *waitfor.Error, got %#v", err)
	}
	if waitErr.ErrorCode() != waitfor.CodeInterrupted {
		t.Errorf("code = %q, want %q", waitErr.ErrorCode(), waitfor.CodeInterrupted)
	}
	if resourceIDOf(err) != "endpoint-1" {
		t.Errorf("error id = %q, want endpoint-1", resourceIDOf(err))
	}
	if !strings.Contains(err.Error(), "runpodctl serverless delete endpoint-1") {
		t.Errorf("error = %q, want the delete command", err.Error())
	}

	wantSignals := map[os.Signal]bool{os.Interrupt: false, syscall.SIGTERM: false}
	for _, signal := range registered {
		wantSignals[signal] = true
	}
	for signal, seen := range wantSignals {
		if !seen {
			t.Errorf("the wait must register for %v", signal)
		}
	}
}

// --wait-timeout on its own would silently not wait; say so instead.
func TestRunCreate_WaitTimeoutWithoutWait(t *testing.T) {
	snapshotCreateFlags(t)
	createWait = false
	createWaitTimeout = "30s"

	client := &mockServerlessCreateClient{}
	oldFactory := newServerlessCreateClient
	newServerlessCreateClient = func() (serverlessCreateClient, error) { return client, nil }
	t.Cleanup(func() { newServerlessCreateClient = oldFactory })
	installMockWaitHealthClient(t, client)

	cmd := mockCreateCommand("wait-timeout")
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	if err := runCreate(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "--wait-timeout has no effect without --wait") {
		t.Errorf("stderr = %q, want the ignored-flag note", stderr.String())
	}
	if client.healthCalls != 0 {
		t.Errorf("health polled %d times, want 0", client.healthCalls)
	}
}

func TestUpdateCmd_Flags(t *testing.T) {
	flags := updateCmd.Flags()

	if flags.Lookup("name") == nil {
		t.Error("expected --name flag")
	}
	if flags.Lookup("workers-min") == nil {
		t.Error("expected --workers-min flag")
	}
	if flags.Lookup("workers-max") == nil {
		t.Error("expected --workers-max flag")
	}
	if flags.Lookup("idle-timeout") == nil {
		t.Error("expected --idle-timeout flag")
	}
	if flags.Lookup("scale-by") == nil {
		t.Error("expected --scale-by flag")
	}
	if flags.Lookup("scale-threshold") == nil {
		t.Error("expected --scale-threshold flag")
	}
}

func TestDeleteCmd_Aliases(t *testing.T) {
	aliases := deleteCmd.Aliases
	hasRm := false
	hasRemove := false
	for _, alias := range aliases {
		if alias == "rm" {
			hasRm = true
		}
		if alias == "remove" {
			hasRemove = true
		}
	}
	if !hasRm {
		t.Error("expected alias 'rm'")
	}
	if !hasRemove {
		t.Error("expected alias 'remove'")
	}
}

func executeCommand(root *cobra.Command, args ...string) (output string, err error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err = root.Execute()
	return buf.String(), err
}

func TestServerlessCmd_Help(t *testing.T) {
	output, err := executeCommand(Cmd, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Error("expected help output")
	}
}

// Each /health read inside a wait must carry its own deadline. The invoke client
// has no client-wide timeout, and the wait loop's context has none, so without a
// per-call bound a single wedged read would hang the whole wait rather than
// counting as one failed poll.
func TestWaitHealthClientBoundsEachRead(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Reset()
	t.Cleanup(viper.Reset)

	// drive it against a server that never answers: the call has to return on its
	// own deadline, not hang, and the caller's context carries no deadline at all.
	blocked := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()
	defer close(blocked)

	t.Setenv(configenv.InvokeURLEnv, server.URL)
	viper.Set("timeout", 100*time.Millisecond)

	client, err := newWaitHealthClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := client.(boundedHealthClient); !ok {
		t.Fatalf("the wait's health client must apply a per-call bound, got %T", client)
	}

	done := make(chan error, 1)
	go func() {
		_, err := client.EndpointHealthCounts(context.Background(), "ep-1")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected the read to fail on its own deadline")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read never returned: it is not bounded by a per-call deadline")
	}
}
