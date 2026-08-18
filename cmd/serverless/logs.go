package serverless

import (
	"context"
	"fmt"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/logstream"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <endpoint-id>",
	Short: "read a serverless endpoint's worker logs",
	Long: `read the logs of the workers backing a serverless endpoint.

output is json lines: one {source,line,ts,workerId} object per line, so it can be
piped straight into jq or read by an agent.

logs belong to a worker, not to the endpoint, so without --worker this resolves
the endpoint's workers and reads all of them at once, tagging each line with its
workerId. pass --worker to read one.

a crash-looping worker is the case this exists for: repeated system "start
container" lines with no container output means the container exits before the
handler runs, which leaves jobs sitting in the queue even though nothing is wrong
with capacity.

without --follow this replays recent lines and exits as soon as they stop
arriving. with --follow it keeps streaming, reconnecting on its own if the
connection drops, and picking up workers that appear while it runs -- so an
endpoint scaling up mid-follow does not need the command re-run.`,
	Example: `  # every worker's recent logs
  runpodctl serverless logs abc123

  # follow one worker
  runpodctl serverless logs abc123 --worker xyz789 --follow

  # the last hour, platform lines only, to see why a worker will not start
  runpodctl serverless logs abc123 --since 1h --source system`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

var (
	logsFlags  logstream.Flags
	logsWorker string
)

func init() {
	logsFlags.Register(logsCmd)
	logsCmd.Flags().StringVar(&logsWorker, "worker", "", "read one worker by id instead of every worker on the endpoint")
}

func runLogs(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	opts, err := logsFlags.Resolve(cmd, time.Now())
	if err != nil {
		return err
	}

	targets, err := resolveLogTargets(endpointID, logsWorker)
	if err != nil {
		return err
	}

	client, err := api.NewLogClient()
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return logstream.Run(client, targets, opts, &logsFlags, format, workerDiscovery(endpointID, logsWorker))
}

// workerDiscovery lets a follow pick up workers that appear after it started.
//
// It returns nil for an explicit --worker: the caller named the one stream they
// want, so quietly attaching others would be the opposite of what they asked for.
func workerDiscovery(endpointID, workerID string) *logstream.Discovery {
	if workerID != "" {
		return nil
	}
	return &logstream.Discovery{
		Refresh: func(ctx context.Context) ([]logstream.Target, error) {
			// Built per poll rather than closed over: NewV2Client reads the api key
			// and base url through configenv, so constructing it at call time keeps
			// a long follow honest about the current config, and http.Client reuses
			// its connection pool through the default transport anyway.
			client, err := api.NewV2Client()
			if err != nil {
				return nil, err
			}
			workers, err := client.ListEndpointWorkers(ctx, endpointID)
			if err != nil {
				return nil, err
			}
			return workerTargets(endpointID, workers), nil
		},
	}
}

// workerTargets maps a worker listing to log routes, skipping any worker with no
// id -- it cannot be addressed, and building a path from it would request
// /workers//logs.
func workerTargets(endpointID string, workers *api.WorkersResponse) []logstream.Target {
	targets := make([]logstream.Target, 0, len(workers.Workers))
	for _, worker := range workers.Workers {
		if worker.ID == "" {
			continue
		}
		targets = append(targets, logstream.Target{
			Path:     api.WorkerLogsPath(endpointID, worker.ID),
			WorkerID: worker.ID,
		})
	}
	return targets
}

// resolveLogTargets turns an endpoint id into the set of worker log routes to
// read. An explicit --worker is taken at face value and costs no api call, so a
// worker id from any source (including one the listing no longer reports) still
// works.
func resolveLogTargets(endpointID, workerID string) ([]logstream.Target, error) {
	if workerID != "" {
		return []logstream.Target{{
			Path:     api.WorkerLogsPath(endpointID, workerID),
			WorkerID: workerID,
		}}, nil
	}

	client, err := api.NewV2Client()
	if err != nil {
		return nil, err
	}
	workers, err := client.ListEndpointWorkersWithTimeout(endpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to list workers for endpoint %s: %w", endpointID, err)
	}

	targets := workerTargets(endpointID, workers)

	if len(targets) == 0 {
		// No workers is not an api failure, but there is nothing to stream and an
		// empty success would read as "no logs". Say which of the two it is,
		// because they have different fixes.
		//
		// Coded `conflict`, not `usage_error`: the endpoint id was correct and
		// nothing the caller typed was wrong, it is the endpoint that is in a
		// state with no logs to give. Reporting a usage error tells an agent its
		// input was bad, so it re-guesses the id instead of following the advice
		// in this very message.
		return nil, &api.APIError{
			Message: fmt.Sprintf("endpoint %s has no workers to read logs from. it may be scaled to zero with no queued jobs, or waiting on gpu capacity -- check `runpodctl serverless health %s`", endpointID, endpointID),
			Code:    "conflict",
		}
	}
	return targets, nil
}
