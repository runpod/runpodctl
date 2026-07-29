package serverless

import (
	"fmt"
	"time"

	"github.com/runpod/runpodctl/internal/clierr"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <endpoint-id> <job-id>",
	Short: "get the status of a serverless job",
	Long: `get the status of a job previously submitted to an endpoint (/status/<job-id>).

this is the follow-up for 'serverless run --no-wait' and for a run that hit its
--wait budget. one check by default (--wait 0); pass --wait to keep polling until
the job is terminal.

exit codes: 0 when the job is COMPLETED or still queued/running, 1 when the job
ended FAILED / CANCELLED / TIMED_OUT or when --wait ran out. the job payload is
printed on stdout either way.

examples:
  runpodctl serverless status <endpoint-id> <job-id>
  runpodctl serverless status <endpoint-id> <job-id> --wait 5m`,
	Args: cobra.ExactArgs(2),
	RunE: runStatus,
}

var statusWait time.Duration

func init() {
	statusCmd.Flags().DurationVar(&statusWait, "wait", 0, "keep polling until the job is terminal, up to this long (0 = check once)")
}

func runStatus(cmd *cobra.Command, args []string) error {
	endpointID, jobID := args[0], args[1]

	if statusWait < 0 {
		return clierr.Usagef("--wait cannot be negative")
	}

	client, err := newInvokeClient()
	if err != nil {
		return err
	}

	cfg := &output.Config{Format: output.ParseFormat(cmd.Flag("output").Value.String())}
	deadline := time.Now().Add(statusWait)

	// with --wait, the first check follows the same transient-failure policy as the
	// poll loop; without it, the deadline has already passed and this is a single
	// fail-fast call.
	job, err := fetchJobStatus(client, endpointID, jobID, deadline, statusWait > 0)
	if err != nil {
		return fmt.Errorf("failed to get job status: %w", err)
	}

	var waitErr error
	if statusWait > 0 {
		job, waitErr = waitForTerminal(client, endpointID, job, deadline)
	}

	if printErr := output.PrintRaw(job.Raw(), cfg); printErr != nil {
		return printErr
	}
	if waitErr != nil {
		return waitErr
	}
	// a queued or running job is a successful answer to "what is its status", so
	// jobOutcome only fails on a terminal non-COMPLETED status.
	return jobOutcome(job)
}
