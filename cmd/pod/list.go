package pod

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list all pods",
	Long:  "list all pods in your account",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

type podListOutput struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	DesiredStatus string      `json:"desiredStatus"`
	ImageName     string      `json:"imageName"`
	GpuID         string      `json:"gpuId,omitempty"`
	GpuCount      int         `json:"gpuCount"`
	VolumeInGb    int         `json:"volumeInGb"`
	CostPerHr     float64     `json:"costPerHr,omitempty"`
	CreatedAt     interface{} `json:"createdAt,omitempty"`
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
	listCmd.Flags().StringVar(&listStatus, "status", "", "filter by desired status (e.g. RUNNING, EXITED)")
	listCmd.Flags().StringVar(&listSince, "since", "", "filter pods created within duration (e.g. 1h, 7d)")
	listCmd.Flags().StringVar(&listCreatedAfter, "created-after", "", "filter pods created after date (e.g. 2025-01-15)")
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "show all pods including exited (default: running only)")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	opts := &api.PodListOptions{
		ComputeType: listComputeType,
		Name:        listName,
	}

	pods, err := client.ListPods(opts)
	if err != nil {
		output.Error(err)
		return err
	}

	// Determine time cutoff from --since and --created-after
	var cutoff time.Time
	if listSince != "" {
		d, err := parseDuration(listSince)
		if err != nil {
			output.Error(err)
			return err
		}
		cutoff = time.Now().Add(-d)
	}
	if listCreatedAfter != "" {
		t, err := time.Parse("2006-01-02", listCreatedAfter)
		if err != nil {
			err = fmt.Errorf("invalid --created-after format, expected YYYY-MM-DD: %w", err)
			output.Error(err)
			return err
		}
		if cutoff.IsZero() || t.After(cutoff) {
			cutoff = t
		}
	}

	statusFilter := strings.ToUpper(listStatus)
	if statusFilter == "" && !listAll {
		statusFilter = "RUNNING"
	}

	items := make([]podListOutput, 0, len(pods))
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

		items = append(items, podListOutput{
			ID:            p.ID,
			Name:          p.Name,
			DesiredStatus: p.DesiredStatus,
			ImageName:     p.ImageName,
			GpuID:         p.GpuTypeID,
			GpuCount:      p.GpuCount,
			VolumeInGb:    p.VolumeInGb,
			CostPerHr:     p.CostPerHr,
			CreatedAt:     p.CreatedAt,
		})
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(items, &output.Config{Format: format})
}

// parseDuration parses a duration string like "1h", "7d", "30d".
// Supported suffixes: h (hours), d (days).
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration %q: too short", s)
	}
	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	switch suffix {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration %q: unsupported suffix %q (use h or d)", s, string(suffix))
	}
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
