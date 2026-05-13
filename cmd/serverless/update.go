package serverless

import (
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update <endpoint-id>",
	Short: "update an endpoint",
	Long:  "update an existing serverless endpoint",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdate,
}

var (
	updateName        string
	updateWorkersMin  int
	updateWorkersMax  int
	updateIdleTimeout int
	updateScaleBy     string
	updateScaleThreshold int
)

func init() {
	updateCmd.Flags().StringVar(&updateName, "name", "", "new endpoint name")
	updateCmd.Flags().IntVar(&updateWorkersMin, "workers-min", -1, "new minimum number of workers")
	updateCmd.Flags().IntVar(&updateWorkersMax, "workers-max", -1, "new maximum number of workers")
	updateCmd.Flags().IntVar(&updateIdleTimeout, "idle-timeout", -1, "new idle timeout in seconds")
	updateCmd.Flags().StringVar(&updateScaleBy, "scale-by", "", "autoscale strategy: delay (seconds of queue wait) or requests (pending request count)")
	updateCmd.Flags().IntVar(&updateScaleThreshold, "scale-threshold", -1, "trigger point for autoscaler (delay: seconds, requests: count)")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	req := &api.EndpointUpdateRequest{}

	if updateName != "" {
		req.Name = updateName
	}
	if updateWorkersMin >= 0 {
		req.WorkersMin = updateWorkersMin
	}
	if updateWorkersMax >= 0 {
		req.WorkersMax = updateWorkersMax
	}
	if updateIdleTimeout >= 0 {
		req.IdleTimeout = updateIdleTimeout
	}
	if updateScaleBy != "" {
		switch strings.ToLower(strings.TrimSpace(updateScaleBy)) {
		case "delay":
			req.ScalerType = "QUEUE_DELAY"
		case "requests":
			req.ScalerType = "REQUEST_COUNT"
		default:
			return fmt.Errorf("invalid --scale-by %q (use delay or requests)", updateScaleBy)
		}
	}
	if updateScaleThreshold >= 0 {
		req.ScalerValue = updateScaleThreshold
	}

	endpoint, err := client.UpdateEndpoint(endpointID, req)
	if err != nil {
		output.Error(err)
		return fmt.Errorf("failed to update endpoint: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}
