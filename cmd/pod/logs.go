package pod

import (
	"time"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/logstream"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <pod-id>",
	Short: "read a pod's logs",
	Long: `read a pod's container and system logs.

output is json lines: one {source,line,ts} object per line, so it can be piped
straight into jq or read by an agent.

source is either container (your workload's own output) or system (the platform
narrating image pull, container create and start). a stalled deploy usually shows
up in the system lines: repeated pull progress, or a create that never reaches
start.

without --follow this replays recent lines and exits as soon as they stop
arriving. with --follow it keeps streaming, reconnecting on its own if the
connection drops.`,
	Example: `  # last 100 lines, then exit
  runpodctl pod logs abc123

  # follow live output
  runpodctl pod logs abc123 --follow

  # the last 30 minutes of platform lines only
  runpodctl pod logs abc123 --since 30m --source system

  # live output only, no history
  runpodctl pod logs abc123 --tail 0 --follow`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

var logsFlags logstream.Flags

func init() {
	logsFlags.Register(logsCmd)
}

func runLogs(cmd *cobra.Command, args []string) error {
	opts, err := logsFlags.Resolve(cmd, time.Now())
	if err != nil {
		return err
	}

	client, err := api.NewLogClient()
	if err != nil {
		return err
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	targets := []logstream.Target{{Path: api.PodLogsPath(args[0])}}
	return logstream.Run(client, targets, opts, &logsFlags, format)
}
