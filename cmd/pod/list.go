package pod

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/duration"
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
		d, err := duration.Parse(listSince)
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

	items := make([]podListOutput, 0, len(matched))
	for i := range matched {
		p := matched[i]
		ct := parseCreatedAt(p.CreatedAt)
		var createdAtStr string
		if !ct.IsZero() {
			createdAtStr = ct.UTC().Format(time.RFC3339)
		}

		// rest v2 reports runtime telemetry in the same read, so status and
		// telemetry come from one snapshot. `pod get` and both ssh paths use the
		// same derivation.
		state := podstate.Derive(podstate.Signals{DesiredStatus: p.DesiredStatus})
		var runtime *api.LegacyRuntime
		lastStatusChange := p.LastStatusChange
		if view := p.Legacy(); view != nil {
			state = sshconnect.PodState(view)
			runtime = view.Runtime
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
	// the api's own layout, which pod reads print as createdAt (go's
	// time.String, e.g. "2026-10-02 05:28:38.366 +0000 UTC")
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
		return t
	}
	// Try Unix timestamp string
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(ts, 0)
	}
	return time.Time{}
}
