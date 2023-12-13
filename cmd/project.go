package cmd

import (
	"cli/cmd/project"

	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project [command]",
	Short: "manage projects",
	Long:  "Project management for Runpod projects",
}

func init() {
	projectCmd.AddCommand(project.NewProjectCmd)
	projectCmd.AddCommand(project.StartProjectCmd)
	projectCmd.AddCommand(project.DeployProjectCmd)
	// projectCmd.AddCommand(project.BuildProjectCmd)
}
