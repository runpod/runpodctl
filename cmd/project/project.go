package pods

import (
	"github.com/spf13/cobra"
)

var NewProjectCmd = &cobra.Command{
	Use:   "new",
	Args:  cobra.ExactArgs(0),
	Short: "create a new project",
	Long:  "create a new Runpod project folder",
	Run: func(cmd *cobra.Command, args []string) {
		//prompt project name (if not provided)

		//select network volume
		//select CUDA version
		//select Python version
		//create files
		//folder structure (check for --init)
		//project toml
	},
}

var StartProjectCmd = &cobra.Command{
	Use:   "start",
	Args:  cobra.ExactArgs(0),
	Short: "start current project",
	Long:  "start a development pod session for the Runpod project in the current folder",
	Run: func(cmd *cobra.Command, args []string) {
		//parse project toml
		//check for existing pod or
		//try to get pod with one of gpu types
		//open ssh connection
		//create remote folder structure
		//rsync project files
		//activate venv on remote
		//create file watcher
		//run launch api server / hot reload loop
	},
}

var DeployProjectCmd = &cobra.Command{
	Use:   "deploy",
	Args:  cobra.ExactArgs(0),
	Short: "deploy current project",
	Long:  "deploy an endpoint for the Runpod project in the current folder",
	Run: func(cmd *cobra.Command, args []string) {
		//parse project toml
		//check for existing pod or
		//try to get pod with one of gpu types
		//open ssh connection
		//sync remote dev to remote prod
		//deploy new template
		//deploy / update endpoint
	},
}

func init() {
}
