package pod

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"
	"github.com/runpod/runpodctl/internal/podstate"
	"github.com/runpod/runpodctl/internal/sshconnect"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list all pods",
	Long: `list all pods in your account.

defaults to running pods only; use --all to include stopped ones.

runtimeStatus reports what each pod is actually doing, which desiredStatus
cannot: running (container up and reporting), initializing (no container
reported yet - image pull, create or boot), stopped, terminated, or unknown
(not derivable, read desiredStatus). runtimeStatusReason carries a stable
token when there is more to say, and lastStatusChange carries the backend's
raw text.`,
	Args: cobra.NoArgs,
	RunE: runList,
}

type podListOutput struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	DesiredStatus       string  `json:"desiredStatus"`
	RuntimeStatus       string  `json:"runtimeStatus"`
	RuntimeStatusReason string  `json:"runtimeStatusReason,omitempty"`
	ImageName           string  `json:"imageName"`
	GpuID               string  `json:"gpuId,omitempty"`
	GpuCount            int     `json:"gpuCount"`
	VolumeInGb          int     `json:"volumeInGb"`
	CostPerHr           float64 `json:"costPerHr,omitempty"`
	CreatedAt           string  `json:"createdAt,omitempty"`
	UptimeSeconds       *int    `json:"uptimeSeconds,omitempty"`
	// LastStatusChange is the backend's free-text note about the last
	// transition ("Rented by User: ...", "Exited by user: ...", "Outbid: ..."),
	// which runtimeStatusReason is a lossy tokenisation of. It is carried here
	// so a phrasing this cli does not recognise still reaches the caller,
	// instead of leaving `pod list` with no explanation at all.
	LastStatusChange string `json:"lastStatusChange,omitempty"`
}

var (
	listComputeType  string
	listName         string
	listStatus       string
	listSince        string
	listCreatedAfter string
	listAll          bool
)

func init() {
	listCmd.Flags().StringVar(&listComputeType, "compute-type", "", "filter by compute type (GPU or CPU)")
	listCmd.Flags().StringVar(&listName, "name", "", "filter by pod name")
	listCmd.Flags().StringVar(&listStatus, "status", "", "filter by desired status (e.g. RUNNING, EXITED); not runtimeStatus values like initializing")
	listCmd.Flags().StringVar(&listSince, "since", "", "filter pods created within duration (e.g. 1h, 7d)")
	listCmd.Flags().StringVar(&listCreatedAfter, "created-after", "", "filter pods created after date (e.g. 2025-01-15)")
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "show all pods including exited (default: running only)")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	opts := &api.PodListOptions{
		ComputeType: listComputeType,
		Name:        listName,
	}

	pods, err := client.ListPods(opts)
	if err != nil {
		return err
	}

	// Determine time cutoff from --since and --created-after
	var cutoff time.Time
	if listSince != "" {
		d, err := parseDuration(listSince)
		if err != nil {
			return err
		}
		cutoff = time.Now().Add(-d)
	}
	if listCreatedAfter != "" {
		t, err := time.Parse("2006-01-02", listCreatedAfter)
		if err != nil {
			err = fmt.Errorf("invalid --created-after format, expected YYYY-MM-DD: %w", err)
			return err
		}
		if cutoff.IsZero() || t.After(cutoff) {
			cutoff = t
		}
	}

	if listSince != "" && listCreatedAfter != "" {
		err := fmt.Errorf("--since and --created-after cannot be used together")
		return err
	}

	if listAll && listStatus != "" {
		err := fmt.Errorf("--all and --status cannot be used together")
		return err
	}

	statusFilter := listStatus
	if statusFilter == "" && !listAll {
		statusFilter = "RUNNING"
	}

	// Filter first: the runtime side-call below is decoration, and must not be
	// paid for when there is nothing left to decorate.
	matched := make([]api.Pod, 0, len(pods))
	for _, p := range pods {
		if statusFilter != "" && !strings.EqualFold(p.DesiredStatus, statusFilter) {
			continue
		}
		if !cutoff.IsZero() {
			created := parseCreatedAt(p.CreatedAt)
			if created.IsZero() || created.Before(cutoff) {
				continue
			}
		}
		matched = append(matched, p)
	}

	var runtimes map[string]*api.LegacyPod
	if needsRuntimeProbe(matched) {
		runtimes = fetchRuntimes()
	}

	items := make([]podListOutput, 0, len(matched))
	for _, p := range matched {
		ct := parseCreatedAt(p.CreatedAt)
		var createdAtStr string
		if !ct.IsZero() {
			createdAtStr = ct.UTC().Format(time.RFC3339)
		}

		// Without the graphql snapshot nothing was probed. A pod rest lists that
		// `myself.pods` omits tells us nothing about its container, and calling
		// that "initializing" would be a claim we never checked — `pod get`
		// reports unknown for the same pod.
		state := podstate.Derive(podstate.Signals{
			DesiredStatus:    p.DesiredStatus,
			LastStatusChange: p.LastStatusChange,
		})
		var runtime *api.LegacyRuntime
		lastStatusChange := p.LastStatusChange
		if gqlPod, ok := runtimes[p.ID]; ok {
			// The same single derivation `pod get` and both ssh paths use, fed
			// from the snapshot the runtime block came from. rest's
			// desiredStatus is still what we *publish*, but gating graphql
			// telemetry on the other surface's status lets momentary skew report
			// `running` plus a dead container's uptime for a pod graphql already
			// calls EXITED, and contradict `pod get` in the same second.
			state = sshconnect.PodState(gqlPod)
			runtime = gqlPod.Runtime
			if lastStatusChange == nil {
				lastStatusChange = gqlPod.LastStatusChange
			}
		}
		var uptime *int
		if state.Status == podstate.StatusRunning && runtime != nil {
			// gated on running: stale telemetry outlives a stopped container.
			uptime = runtime.UptimeInSeconds
		}

		items = append(items, podListOutput{
			ID:                  p.ID,
			Name:                p.Name,
			DesiredStatus:       p.DesiredStatus,
			RuntimeStatus:       string(state.Status),
			RuntimeStatusReason: string(state.Reason),
			ImageName:           p.ImageName,
			GpuID:               p.GpuTypeID,
			GpuCount:            p.GpuCount,
			VolumeInGb:          p.VolumeInGb,
			CostPerHr:           p.CostPerHr,
			CreatedAt:           createdAtStr,
			UptimeSeconds:       uptime,
			LastStatusChange:    statusText(lastStatusChange),
		})
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(items, &output.Config{Format: format})
}

// statusText coerces the api's interface{} lastStatusChange to a string, and to
// "" for anything else so the field is simply omitted.
func statusText(v interface{}) string {
	s, _ := v.(string)
	return s
}

// needsRuntimeProbe reports whether the graphql side-call can change any of the
// rows we are about to print.
//
// podstate only consults runtime telemetry on the RUNNING branch — a stopped or
// terminated pod is derived from lastStatusChange alone, on purpose, because its
// telemetry is stale — so probing a result set with no RUNNING pod in it buys
// nothing and can still cost the full 5s cap. `pod list --all` on an account of
// stopped pods is the common shape of that, and it is exactly the case an
// unresponsive graphql would otherwise have made slow for no reason.
func needsRuntimeProbe(matched []api.Pod) bool {
	for _, p := range matched {
		if strings.EqualFold(strings.TrimSpace(p.DesiredStatus), "RUNNING") {
			return true
		}
	}
	return false
}

// runtimeProbeTimeout bounds the runtime side-call. `pod list` is the hottest
// read command and is polled in loops; before CON-690 it never touched graphql
// at all, so an unresponsive graphql must not be able to turn a ~100ms list into
// a 30s stall (the default graphqlTimeout) for the sake of a decorative field.
const runtimeProbeTimeout = 5 * time.Second

// fetchRuntimes returns runtime telemetry keyed by pod id, or nil when it could
// not be obtained.
//
// `pod list` runs on rest /pods, which never returns `runtime`, so a second call
// is unavoidable. It is deliberately the *bulk* graphql myPods query — one
// request for every pod, never one per pod — and it is best-effort: a failure
// downgrades runtimeStatus to unknown/runtime_unavailable rather than failing a
// list that otherwise succeeded. A pod missing from the returned map is treated
// as "not probed", which podstate reports as unknown rather than as a container
// that is down.
func fetchRuntimes() map[string]*api.LegacyPod {
	gqlClient, err := api.NewGraphQLClient()
	if err != nil {
		return nil
	}
	gqlClient.LimitTimeout(runtimeProbeTimeout)
	pods, err := gqlClient.GetPods()
	if err != nil {
		return nil
	}
	byID := make(map[string]*api.LegacyPod, len(pods))
	for _, p := range pods {
		if p != nil {
			byID[p.ID] = p
		}
	}
	return byID
}

// parseDuration parses a duration string like "30m", "1h", "1h30m", "7d".
// It handles "d" (days) specially and falls back to time.ParseDuration for everything else.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		if n <= 0 {
			return 0, fmt.Errorf("invalid duration %q: must be positive", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: supported formats are e.g. 30m, 2h, 7d", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid duration %q: must be positive", s)
	}
	return d, nil
}

// parseCreatedAt parses the createdAt field from the API response.
// It handles RFC3339 strings and Unix timestamp strings.
func parseCreatedAt(v interface{}) time.Time {
	s, ok := v.(string)
	if !ok {
		return time.Time{}
	}
	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try Unix timestamp string
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(ts, 0)
	}
	return time.Time{}
}
