package cmd

import (
	"cli/cmd/project"

	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project [command]",
	Short: "Rapidly develop and deploy a project entirely on RunPod's infrastructure without touching Docker.",
	Long:  "",
}

func init() {
	projectCmd.AddCommand(project.NewProjectCmd)
	projectCmd.AddCommand(project.StartProjectCmd)
	projectCmd.AddCommand(project.DeployProjectCmd)
	projectCmd.AddCommand(project.BuildProjectCmd)
}
