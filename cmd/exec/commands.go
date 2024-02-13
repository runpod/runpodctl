package exec

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var RemotePythonCmd = &cobra.Command{
	Use:   "python [file]",
	Short: "Runs a remote Python shell",
	Long:  `Runs a remote Python shell with a local script file.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		podID, _ := cmd.Flags().GetString("pod_id")
		file := args[0]

		// Default to the session pod if no pod_id is provided
		// if podID == "" {
		// 	var err error
		// 	podID, err = api.GetSessionPod()
		// 	if err != nil {
		// 		fmt.Fprintf(os.Stderr, "Error retrieving session pod: %v\n", err)
		// 		return
		// 	}
		// }

		fmt.Println("Running remote Python shell...")
		if err := PythonOverSSH(podID, file); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing Python over SSH: %v\n", err)
		}
	},
}

func init() {
	RemotePythonCmd.Flags().String("pod_id", "", "The ID of the pod to run the command on.")
	RemotePythonCmd.MarkFlagRequired("file")
}
