package legacy

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// These are hidden legacy commands that redirect to new syntax
// They detect old-style verb-noun commands and show deprecation warning

func printDeprecationWarning(oldCmd, newCmd string) {
	fmt.Fprintf(os.Stderr, "warning: '%s' is deprecated, use '%s' instead\n", oldCmd, newCmd)
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
	Short:  "deprecated: use 'runpod <resource> start <id>'",
}

// StopCmd is the legacy 'stop' command
var StopCmd = &cobra.Command{
	Use:    "stop",
	Hidden: true,
	Short:  "deprecated: use 'runpod <resource> stop <id>'",
}

// Legacy pod subcommands
var legacyGetPodCmd = &cobra.Command{
	Use:   "pod [id]",
	Short: "deprecated: use 'runpod pod list' or 'runpod pod get <id>'",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			printDeprecationWarning("runpod get pod", "runpod pod list")
			// Execute: runpod pod list
			newCmd := cmd.Root().Commands()
			for _, c := range newCmd {
				if c.Use == "pod" {
					for _, sub := range c.Commands() {
						if sub.Use == "list" {
							sub.Run(sub, args)
							return
						}
					}
				}
			}
		} else {
			printDeprecationWarning("runpod get pod <id>", "runpod pod get <id>")
			// Execute: runpod pod get <id>
			newCmd := cmd.Root().Commands()
			for _, c := range newCmd {
				if c.Use == "pod" {
					for _, sub := range c.Commands() {
						if sub.Use == "get <pod-id>" {
							sub.Run(sub, args)
							return
						}
					}
				}
			}
		}
	},
}

var legacyCreatePodCmd = &cobra.Command{
	Use:   "pod",
	Short: "deprecated: use 'runpod pod create'",
	Run: func(cmd *cobra.Command, args []string) {
		printDeprecationWarning("runpod create pod", "runpod pod create")
		fmt.Fprintln(os.Stderr, "use: runpod pod create --image=<image> [flags]")
	},
}

var legacyRemovePodCmd = &cobra.Command{
	Use:   "pod <id>",
	Short: "deprecated: use 'runpod pod delete <id>'",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		printDeprecationWarning("runpod remove pod <id>", "runpod pod delete <id>")
		// Execute: runpod pod delete <id>
		newCmd := cmd.Root().Commands()
		for _, c := range newCmd {
			if c.Use == "pod" {
				for _, sub := range c.Commands() {
					if sub.Use == "delete <pod-id>" {
						sub.Run(sub, args)
						return
					}
				}
			}
		}
	},
}

var legacyStartPodCmd = &cobra.Command{
	Use:   "pod <id>",
	Short: "deprecated: use 'runpod pod start <id>'",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		printDeprecationWarning("runpod start pod <id>", "runpod pod start <id>")
		// Execute: runpod pod start <id>
		newCmd := cmd.Root().Commands()
		for _, c := range newCmd {
			if c.Use == "pod" {
				for _, sub := range c.Commands() {
					if sub.Use == "start <pod-id>" {
						sub.Run(sub, args)
						return
					}
				}
			}
		}
	},
}

var legacyStopPodCmd = &cobra.Command{
	Use:   "pod <id>",
	Short: "deprecated: use 'runpod pod stop <id>'",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		printDeprecationWarning("runpod stop pod <id>", "runpod pod stop <id>")
		// Execute: runpod pod stop <id>
		newCmd := cmd.Root().Commands()
		for _, c := range newCmd {
			if c.Use == "pod" {
				for _, sub := range c.Commands() {
					if sub.Use == "stop <pod-id>" {
						sub.Run(sub, args)
						return
					}
				}
			}
		}
	},
}

func init() {
	GetCmd.AddCommand(legacyGetPodCmd)
	CreateCmd.AddCommand(legacyCreatePodCmd)
	RemoveCmd.AddCommand(legacyRemovePodCmd)
	StartCmd.AddCommand(legacyStartPodCmd)
	StopCmd.AddCommand(legacyStopPodCmd)
}
