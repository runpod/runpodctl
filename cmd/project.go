package cmd

import (
	"github.com/runpod/runpodctl/cmd/project"

	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project [command]",
	Short: "manage runpod projects",
	Long:  "develop and deploy projects entirely on runpod's infrastructure.",
}

func init() {
	projectCmd.AddCommand(project.NewProjectCmd)
	projectCmd.AddCommand(project.StartProjectCmd)
	projectCmd.AddCommand(project.DeployProjectCmd)
	projectCmd.AddCommand(project.BuildProjectCmd)
}
