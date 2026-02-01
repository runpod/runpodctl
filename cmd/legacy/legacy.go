package legacy

import (
	"fmt"
	"os"

	"github.com/runpod/runpod/cmd/pod"
	"github.com/spf13/cobra"
)

// These are hidden legacy commands that provide backwards compatibility
// They show deprecation warnings but execute the same functionality

func wrapWithDeprecation(cmd *cobra.Command, oldSyntax, newSyntax string) {
	originalPreRun := cmd.PreRun
	originalPreRunE := cmd.PreRunE

	cmd.PreRun = nil
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		fmt.Fprintf(os.Stderr, "warning: '%s' is deprecated, use '%s' instead\n", oldSyntax, newSyntax)
		if originalPreRunE != nil {
			return originalPreRunE(c, args)
		}
		if originalPreRun != nil {
			originalPreRun(c, args)
		}
		return nil
	}
}

// GetCmd is the legacy 'get' command
var GetCmd = &cobra.Command{
	Use:    "get",
	Hidden: true,
	Short:  "deprecated: use 'runpod <resource> list' or 'runpod <resource> get <id>'",
}

// CreateCmd is the legacy 'create' command
var CreateCmd = &cobra.Command{
	Use:    "create",
	Hidden: true,
	Short:  "deprecated: use 'runpod <resource> create'",
}

// RemoveCmd is the legacy 'remove' command
var RemoveCmd = &cobra.Command{
	Use:    "remove",
	Hidden: true,
	Short:  "deprecated: use 'runpod <resource> delete <id>'",
}

// StartCmd is the legacy 'start' command
var StartCmd = &cobra.Command{
	Use:    "start",
	Hidden: true,
	Short:  "deprecated: use 'runpod pod start <id>'",
}

// StopCmd is the legacy 'stop' command
var StopCmd = &cobra.Command{
	Use:    "stop",
	Hidden: true,
	Short:  "deprecated: use 'runpod pod stop <id>'",
}

func init() {
	// Use the actual old commands but wrap them with deprecation warnings
	
	// get pod - use the old GetPodCmd which has --allfields support
	getPodCmd := *pod.GetPodCmd // copy the command
	wrapWithDeprecation(&getPodCmd, "runpod get pod", "runpod pod list")
	GetCmd.AddCommand(&getPodCmd)

	// create pod - use the old CreatePodCmd
	createPodCmd := *pod.CreatePodCmd
	wrapWithDeprecation(&createPodCmd, "runpod create pod", "runpod pod create")
	CreateCmd.AddCommand(&createPodCmd)

	// remove pod - use the old RemovePodCmd
	removePodCmd := *pod.RemovePodCmd
	wrapWithDeprecation(&removePodCmd, "runpod remove pod", "runpod pod delete <id>")
	RemoveCmd.AddCommand(&removePodCmd)

	// start pod - use the old StartPodCmd
	startPodCmd := *pod.StartPodCmd
	wrapWithDeprecation(&startPodCmd, "runpod start pod", "runpod pod start <id>")
	StartCmd.AddCommand(&startPodCmd)

	// stop pod - use the old StopPodCmd
	stopPodCmd := *pod.StopPodCmd
	wrapWithDeprecation(&stopPodCmd, "runpod stop pod", "runpod pod stop <id>")
	StopCmd.AddCommand(&stopPodCmd)
}
