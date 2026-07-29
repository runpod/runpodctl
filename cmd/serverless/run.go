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

the payload must be a json object and is sent as {"input": <your json>}; pass
only the handler payload.

the job is submitted on /run and then polled on /status until it is terminal.
/runsync is deliberately not used: it only holds the connection for 90 seconds
and then hands back a still-running job, and until it answers there is no job id
to poll, so a slow response would leave a running job unreachable.

waiting is bounded by --wait (default 5m). when it runs out the job is still
running server-side: the last payload is printed on stdout, a "timeout" error on
stderr names the 'serverless status' command to poll it, and the exit code is 1.

exit codes: 0 when the job is COMPLETED, and when --wait 0 / --no-wait submitted
it successfully. 1 when the request fails, when the wait budget runs out, or when
the job ends FAILED / CANCELLED / TIMED_OUT. the job payload (including the
worker's own error) is still printed on stdout in every one of those cases.

examples:
  # invoke and wait for the result
  runpodctl serverless run <endpoint-id> --input '{"prompt":"hello"}'

  # big payloads: skip shell quoting entirely
  runpodctl serverless run <endpoint-id> --input-file payload.json
  cat payload.json | runpodctl serverless run <endpoint-id> --input -

  # give a cold or slow endpoint longer
  runpodctl serverless run <endpoint-id> --input '{}' --wait 15m

  # submit and get the job id back immediately
  runpodctl serverless run <endpoint-id> --input '{}' --no-wait
  runpodctl serverless status <endpoint-id> <job-id>`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
}

var (
	runInput     string
	runInputFile string
	runNoWait    bool
	runWait      time.Duration
)

func init() {
	runCmd.Flags().StringVar(&runInput, "input", "", "json payload for the handler; '-' reads stdin")
	runCmd.Flags().StringVar(&runInputFile, "input-file", "", "read the json payload from a file; '-' reads stdin")
	runCmd.Flags().BoolVar(&runNoWait, "no-wait", false, "submit and print the job id without waiting (same as --wait 0)")
	runCmd.Flags().DurationVar(&runWait, "wait", api.DefaultInvokeWait, "how long to wait for a terminal job status; 0 does not wait (e.g. 90s, 10m)")
}

func runRun(cmd *cobra.Command, args []string) error {
	endpointID := args[0]

	input, err := resolveJobInput(cmd.InOrStdin(), runInput, runInputFile)
	if err != nil {
		return err
	}
	if runWait < 0 {
		return clierr.Usagef("--wait cannot be negative")
	}
	wait := runWait
	if runNoWait {
		// --no-wait is the discoverable spelling of "do not poll"; keeping it as
		// exactly --wait 0 means there is one waiting code path, and --wait 0 means
		// the same thing on 'run' and on 'status'.
		wait = 0
	}

	client, err := newInvokeClient()
	if err != nil {
		return err
	}

	cfg := &output.Config{Format: output.ParseFormat(cmd.Flag("output").Value.String())}
	deadline := time.Now().Add(wait)

	job, waitErr := invokeJob(client, endpointID, input, deadline, wait > 0)
	if job != nil {
		if printErr := output.PrintRaw(job.Raw(), cfg); printErr != nil {
			return printErr
		}
	}
	if waitErr != nil {
		return waitErr
	}
	return jobOutcome(job)
}

// invokeJob submits the job on /run and, when asked to wait, polls until it is
// terminal. The job it returns is the last payload seen, so the caller can print
// it even when the wait failed.
func invokeJob(client invokeClient, endpointID string, input json.RawMessage, deadline time.Time, wait bool) (*api.Job, error) {
	// inside a wait the submit must not outlive the budget; without one it gets the
	// ordinary per-call timeout.
	submitTimeout := requestTimeout()
	if wait {
		submitTimeout = boundedRequestTimeout(deadline)
	}
	ctx, cancel := context.WithTimeout(context.Background(), submitTimeout)
	defer cancel()

	job, err := client.Run(ctx, endpointID, input)
	if err != nil {
		var timeoutErr *api.TimeoutError
		if errors.As(err, &timeoutErr) {
			// the submit itself timed out, so we never got an id: the job may or may
			// not exist. Say that plainly instead of pointing at a status command
			// there is no id for, or implying a blind re-invoke is safe.
			return nil, api.NewTimeoutError(
				"submitting the job timed out before the invoke api answered, so it is unknown whether the job was created and there is no job id to poll; check 'runpodctl serverless health %s' for queued or running work before invoking again",
				endpointID,
			)
		}
		return nil, fmt.Errorf("failed to submit job: %w", err)
	}

	if !wait {
		// submission succeeded, which is the whole contract of --no-wait: the job is
		// queued and the id on stdout is what 'serverless status' needs. Without an
		// id there is nothing to follow up on, so say nothing rather than printing a
		// command with a hole in it (jobOutcome turns that into a failure).
		if job.ID != "" {
			notef("submitted job %s (%s); poll it with: runpodctl serverless status %s %s", job.ID, job.Status, endpointID, job.ID)
		}
		return job, nil
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

	// the invoke api decodes the request body into a struct whose "input" is a json
	// object, so an array or a scalar is rejected server-side with a 400. catching
	// that here costs nothing and turns a round trip into a local usage_error that
	// names the flag.
	obj, isObject := parsed.(map[string]interface{})
	if !isObject && parsed != nil {
		return nil, clierr.Usagef("payload from %s must be a json object (the invoke api nests it under \"input\"), got %s", source, jsonKind(parsed))
	}

	// a payload that is *only* an "input" key is almost always the whole request
	// envelope pasted in from curl, which would be sent as {"input":{"input":…}}
	// and reach the handler double-wrapped. warn rather than unwrap: guessing
	// would break a handler whose payload genuinely has one "input" field.
	if len(obj) == 1 {
		if _, wrapped := obj["input"]; wrapped {
			notef(`note: payload from %s is just {"input":…}; it is sent as {"input": <payload>}, so pass only the handler payload`, source)
		}
	}

	return json.RawMessage(trimmed), nil
}

// jsonKind names a json value's type for a usage error.
func jsonKind(value interface{}) string {
	switch value.(type) {
	case []interface{}:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return "a scalar"
	}
}
