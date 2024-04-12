package cmd

import (
	"github.com/runpod/runpodctl/cmd/exec"

	"github.com/spf13/cobra"
)

// execCmd represents the base command for executing commands in a pod
var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Execute commands in a pod",
	Long:  `Execute a local file remotely in a pod.`,
}

func init() {
	execCmd.AddCommand(exec.RemotePythonCmd)
}
