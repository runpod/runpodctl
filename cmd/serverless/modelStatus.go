package serverless

import (
	"fmt"

	"github.com/runpod/runpodctl/api"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var modelStatusCmd = &cobra.Command{
	Use:   "model-status <endpoint-id>",
	Short: "diagnose model repo assignment and mount status for an endpoint's workers",
	Long: `show a serverless endpoint's configured model references and, for each
worker, the resolved model version hash, machine assignment status, failure
phase and reason (if any), and expected mount path.

this exists to let you diagnose why a model repo-backed endpoint is stuck
loading or failing to mount a model, without ssh access or internal host logs.

a status of FAILED carries a failurePhase (download_failed, mount_failed,
startup_failed, or cuda_failed) and a failureReason summarizing what went
wrong. for the full free-text detail behind that summary, follow up with
` + "`runpodctl serverless logs <endpoint-id> --source system`" + `.

not covered here: download/mount progress percentage, and the container's
effective MODEL_NAME/MODEL_REVISION environment variables -- neither is
exposed by any api today.`,
	Example: `  # every worker's model assignment status for an endpoint
  runpodctl serverless model-status abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runModelStatus,
}

func init() {
	Cmd.AddCommand(modelStatusCmd)
}

// WorkerModelDiagnostics is one worker's model-repo diagnostics: its current
// status from the v2 worker listing, plus the model versions it requires with
// their machine-level assignment diagnostics. Error is set instead of
// ModelVersions when the per-worker lookup failed, so one bad worker cannot
// blank out every other worker's diagnostics in the same response.
type WorkerModelDiagnostics struct {
	WorkerID      string                 `json:"workerId"`
	WorkerStatus  string                 `json:"workerStatus"`
	ModelVersions []*api.PodModelVersion `json:"modelVersions,omitempty"`
	Error         string                 `json:"error,omitempty"`
}

// ModelStatusResult is the output of `runpodctl serverless model-status`.
type ModelStatusResult struct {
	EndpointID      string                   `json:"endpointId"`
	ModelReferences []string                 `json:"modelReferences"`
	Workers         []WorkerModelDiagnostics `json:"workers"`
}

func runModelStatus(cmd *cobra.Command, args []string) error {
	result, err := buildModelStatusResult(args[0])
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(result, &output.Config{Format: format})
}

// buildModelStatusResult assembles the model-status diagnostics for an
// endpoint: its configured model references, plus each current worker's
// resolved model versions and machine assignment diagnostics. Exported for
// unit testing (lowercase, package-local -- same pattern as resolveLogTargets
// in logs.go).
func buildModelStatusResult(endpointID string) (*ModelStatusResult, error) {
	modelReferences, err := api.GetEndpointModelReferences(endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to get model references for endpoint %s: %w", endpointID, err)
	}

	v2Client, err := internalapi.NewV2Client()
	if err != nil {
		return nil, err
	}
	workersResp, err := v2Client.ListEndpointWorkersWithTimeout(endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to list workers for endpoint %s: %w", endpointID, err)
	}

	result := &ModelStatusResult{
		EndpointID:      endpointID,
		ModelReferences: modelReferences,
		Workers:         make([]WorkerModelDiagnostics, 0, len(workersResp.Workers)),
	}

	for _, worker := range workersResp.Workers {
		if worker.ID == "" {
			// Cannot be addressed by a pod-scoped query; same guard as
			// workerTargets in logs.go.
			continue
		}

		diagnostics := WorkerModelDiagnostics{
			WorkerID:     worker.ID,
			WorkerStatus: worker.Status,
		}

		versions, err := api.GetPodModelVersions(worker.ID)
		if err != nil {
			// One worker's lookup failing (e.g. it terminated between the
			// list call and this one) should not blank out every other
			// worker's diagnostics -- report it inline instead.
			diagnostics.Error = err.Error()
		} else {
			diagnostics.ModelVersions = versions
		}

		result.Workers = append(result.Workers, diagnostics)
	}

	return result, nil
}
