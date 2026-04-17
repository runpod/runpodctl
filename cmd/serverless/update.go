package serverless

import (
	"fmt"
	"os"

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
	updateTemplateID  string
	updateWorkersMin  int
	updateWorkersMax  int
	updateIdleTimeout int
	updateScalerType  string
	updateScalerValue int
)

func init() {
	updateCmd.Flags().StringVar(&updateName, "name", "", "new endpoint name")
	updateCmd.Flags().StringVar(&updateTemplateID, "template-id", "", "new template id")
	updateCmd.Flags().IntVar(&updateWorkersMin, "workers-min", -1, "new minimum number of workers")
	updateCmd.Flags().IntVar(&updateWorkersMax, "workers-max", -1, "new maximum number of workers")
	updateCmd.Flags().IntVar(&updateIdleTimeout, "idle-timeout", -1, "new idle timeout in seconds")
	updateCmd.Flags().StringVar(&updateScalerType, "scaler-type", "", "scaler type (QUEUE_DELAY or REQUEST_COUNT)")
	updateCmd.Flags().IntVar(&updateScalerValue, "scaler-value", -1, "scaler value")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	client, err := api.NewClient()
	if err != nil {
		return err
	}

	req := &api.EndpointUpdateRequest{}
	hasRESTUpdate := false

	if updateName != "" {
		req.Name = updateName
		hasRESTUpdate = true
	}
	if updateWorkersMin >= 0 {
		req.WorkersMin = updateWorkersMin
		hasRESTUpdate = true
	}
	if updateWorkersMax >= 0 {
		req.WorkersMax = updateWorkersMax
		hasRESTUpdate = true
	}
	if updateIdleTimeout >= 0 {
		req.IdleTimeout = updateIdleTimeout
		hasRESTUpdate = true
	}
	if updateScalerType != "" {
		req.ScalerType = updateScalerType
		hasRESTUpdate = true
	}
	if updateScalerValue >= 0 {
		req.ScalerValue = updateScalerValue
		hasRESTUpdate = true
	}

	if hasRESTUpdate {
		if _, err := client.UpdateEndpoint(endpointID, req); err != nil {
			return fmt.Errorf("failed to update endpoint: %w", err)
		}
	}

	if updateTemplateID != "" {
		if err := client.UpdateEndpointTemplate(endpointID, updateTemplateID); err != nil {
			if hasRESTUpdate {
				fmt.Fprintln(os.Stderr, "warning: endpoint rest fields were updated, but template swap failed")
			}
			return fmt.Errorf("failed to update endpoint template: %w", err)
		}
	}

	endpoint, err := client.GetEndpoint(endpointID, false, false)
	if err != nil {
		return fmt.Errorf("failed to get updated endpoint: %w", err)
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(endpoint, &output.Config{Format: format})
}
