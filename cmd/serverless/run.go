package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/clierr"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <endpoint-id>",
	Short: "invoke an endpoint and wait for the result",
	Long: `invoke a serverless endpoint with a json payload and wait for the job to finish.

the payload is sent as {"input": <your json>}; pass only the handler payload.

by default the job goes to /runsync. the invoke api stops holding that connection
after about 90 seconds, so a longer job is picked up on /status automatically —
--async only changes the submit route, not whether the cli waits.

waiting is bounded by --wait (default 5m). when it runs out the job is still
running server-side: the last payload is printed on stdout, a "timeout" error on
stderr names the 'serverless status' command to poll it, and the exit code is 1.

exit codes: 0 when the job is COMPLETED, 1 when the request fails, when the wait
budget runs out, or when the job ends FAILED / CANCELLED / TIMED_OUT. the job
payload (including the worker's own error) is still printed on stdout in every
one of those cases.

examples:
  # invoke and wait for the result
  runpodctl serverless run <endpoint-id> --input '{"prompt":"hello"}'

  # big payloads: skip shell quoting entirely
  runpodctl serverless run <endpoint-id> --input-file payload.json
  cat payload.json | runpodctl serverless run <endpoint-id> --input -

  # queue on /run and poll until it finishes
  runpodctl serverless run <endpoint-id> --input '{}' --async

  # queue on /run and get the job id back immediately
  runpodctl serverless run <endpoint-id> --input '{}' --no-wait
  runpodctl serverless status <endpoint-id> <job-id>`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
}

var (
	runInput     string
	runInputFile string
	runAsync     bool
	runNoWait    bool
	runWait      time.Duration
)

func init() {
	runCmd.Flags().StringVar(&runInput, "input", "", "json payload for the handler; '-' reads stdin")
	runCmd.Flags().StringVar(&runInputFile, "input-file", "", "read the json payload from a file; '-' reads stdin")
	runCmd.Flags().BoolVar(&runAsync, "async", false, "submit on /run instead of /runsync, then poll /status until the job is terminal")
	runCmd.Flags().BoolVar(&runNoWait, "no-wait", false, "submit on /run and print the job id without waiting (implies --async)")
	runCmd.Flags().DurationVar(&runWait, "wait", api.DefaultInvokeWait, "how long to wait for a terminal job status (e.g. 90s, 10m)")
}

func runRun(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	input, err := resolveJobInput(cmd.InOrStdin(), runInput, runInputFile)
	if err != nil {
		return err
	}
	if runWait <= 0 && !runNoWait {
		return clierr.Usagef("--wait must be greater than 0")
	}

	client, err := newInvokeClient()
	if err != nil {
		return err
	}

	cfg := &output.Config{Format: output.ParseFormat(cmd.Flag("output").Value.String())}
	deadline := time.Now().Add(runWait)

	job, waitErr := invokeJob(client, endpointID, input, deadline)
	if job != nil {
		if printErr := output.Print(job, cfg); printErr != nil {
			return printErr
		}
	}
	if waitErr != nil {
		return waitErr
	}
	return jobOutcome(job)
}

// invokeJob submits the job and, unless --no-wait was passed, waits for it to
// reach a terminal status. The job it returns is the last payload seen, so the
// caller can print it even when the wait failed.
func invokeJob(client invokeClient, endpointID string, input json.RawMessage, deadline time.Time) (*api.Job, error) {
	if runAsync || runNoWait {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout())
		defer cancel()
		job, err := client.Run(ctx, endpointID, input)
		if err != nil {
			return nil, fmt.Errorf("failed to submit job: %w", err)
		}
		if runNoWait {
			// submission succeeded, which is the whole contract of --no-wait: the
			// job is queued and the id on stdout is what 'serverless status' needs.
			notef("submitted job %s (%s); poll it with: runpodctl serverless status %s %s", job.ID, job.Status, endpointID, job.ID)
			return job, nil
		}
		return waitForTerminal(client, endpointID, job, deadline)
	}

	// runsync: the api holds the connection until the job finishes or ~90s pass,
	// whichever comes first. RunSync caps the request itself; the deadline here is
	// the caller's overall budget.
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	job, err := client.RunSync(ctx, endpointID, input)
	if err != nil {
		var timeoutErr *api.TimeoutError
		if errors.As(err, &timeoutErr) {
			// no job id ever came back, so there is nothing to poll — say so
			// instead of pointing at a status command the caller cannot run.
			return nil, api.NewTimeoutError(
				"runsync request timed out before returning a job id, so the job cannot be polled; rerun with --async to get a job id up front",
			)
		}
		return nil, fmt.Errorf("failed to invoke endpoint: %w", err)
	}
	return waitForTerminal(client, endpointID, job, deadline)
}

// resolveJobInput reads the handler payload from --input, --input-file or stdin
// and validates that it parses as json before anything is sent, so a quoting
// mistake fails locally with a usage_error instead of as an opaque api error.
func resolveJobInput(stdin io.Reader, inline, file string) (json.RawMessage, error) {
	switch {
	case inline != "" && file != "":
		return nil, clierr.Usagef("--input and --input-file are mutually exclusive")
	case inline == "" && file == "":
		return nil, clierr.Usagef(`one of --input or --input-file is required; pass --input '{}' for a handler that takes no input`)
	}

	var (
		raw    []byte
		source string
	)
	switch {
	case inline == "-" || file == "-":
		source = "stdin"
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read payload from stdin: %w", err)
		}
		raw = data
	case file != "":
		source = "--input-file " + file
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read --input-file: %w", err)
		}
		raw = data
	default:
		source = "--input"
		raw = []byte(inline)
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, clierr.Usagef("payload from %s is empty; expected json", source)
	}

	var parsed interface{}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, clierr.Usagef("payload from %s is not valid json: %v", source, err)
	}

	// a payload that is *only* an "input" key is almost always the whole request
	// envelope pasted in from curl, which would be sent as {"input":{"input":…}}
	// and reach the handler double-wrapped. warn rather than unwrap: guessing
	// would break a handler whose payload genuinely has one "input" field.
	if obj, ok := parsed.(map[string]interface{}); ok && len(obj) == 1 {
		if _, wrapped := obj["input"]; wrapped {
			notef(`note: payload from %s is just {"input":…}; it is sent as {"input": <payload>}, so pass only the handler payload`, source)
		}
	}

	return json.RawMessage(trimmed), nil
}
