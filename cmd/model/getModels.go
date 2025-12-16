package model

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var (
	getProvider string
	getName     string
	getAll      bool
)

// GetModelsCmd lists models that are available in the RunPod model repository.
// Hidden while the model repository feature is in development and not ready for general use.
var GetModelsCmd = &cobra.Command{
	Use:    "models",
	Args:   cobra.ExactArgs(0),
	Short:  "list models",
	Long:   "list models available in the RunPod model repository",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		input := &api.GetModelsInput{
			Provider: getProvider,
			Name:     getName,
			All:      getAll,
		}

		models, err := api.GetModels(input)
		if err != nil {
			if errors.Is(err, api.ErrModelRepoNotImplemented) {
				fmt.Println(api.ErrModelRepoNotImplemented.Error())
				return
			}

			cobra.CheckErr(err)
			return
		}

		if len(models) == 0 {
			fmt.Println("no models found")
			return
		}

		displayModels(models)
	},
}

func init() {
	GetModelsCmd.Flags().StringVar(&getProvider, "provider", "", "filter models by provider (e.g. huggingface)")
	GetModelsCmd.Flags().StringVar(&getName, "name", "", "filter models by name")
	GetModelsCmd.Flags().BoolVar(&getAll, "all", false, "list all models available in the RunPod repository")
}

func displayModels(models []*api.Model) {
	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)

	fmt.Fprintln(w, "ID\tProvider\tName\tOwner\tStatus\tCreated At\tUpdated At")
	fmt.Fprintln(w, "--\t--------\t----\t-----\t------\t----------\t----------")

	for _, model := range models {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(model.ID),
			valueOrDash(model.Provider),
			valueOrDash(model.Name),
			valueOrDash(model.Owner),
			valueOrDash(model.Status),
			formatTimestamp(model.CreatedAt),
			formatTimestamp(model.UpdatedAt))
	}

	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to render models table: %v\n", err)
	}
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func formatTimestamp(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}

	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return raw
	}

	switch len(raw) {
	case 10:
		return time.Unix(ts, 0).UTC().Format(time.RFC3339)
	case 13:
		sec := ts / 1_000
		nsec := (ts % 1_000) * int64(time.Millisecond)
		return time.Unix(sec, nsec).UTC().Format(time.RFC3339)
	case 16:
		sec := ts / 1_000_000
		nsec := (ts % 1_000_000) * int64(time.Microsecond)
		return time.Unix(sec, nsec).UTC().Format(time.RFC3339)
	case 19:
		sec := ts / 1_000_000_000
		nsec := ts % 1_000_000_000
		return time.Unix(sec, nsec).UTC().Format(time.RFC3339)
	default:
		if ts > 1_000_000_000_000 {
			sec := ts / 1_000
			nsec := (ts % 1_000) * int64(time.Millisecond)
			return time.Unix(sec, nsec).UTC().Format(time.RFC3339)
		}
		return time.Unix(ts, 0).UTC().Format(time.RFC3339)
	}
}
