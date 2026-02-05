package pods

import (
	"fmt"

	"github.com/runpod/runpodctl/api"

	"github.com/spf13/cobra"
)

var RemovePodsCmd = &cobra.Command{
	Use:   "pods [name]",
	Args:  cobra.ExactArgs(1),
	Short: "remove all pods using name",
	Long:  "remove all pods using name from runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		mypods, err := api.GetPods()
		cobra.CheckErr(err)

		removed := 0
		for _, pod := range mypods {
			if pod.Name == args[0] && removed < podCount {
				_, err := api.RemovePod(pod.Id)
				if err == nil {
					removed++
				}
				cobra.CheckErr(err)
			}
		}

		fmt.Printf(`%d pods removed with name "%s"`, removed, args[0])
		fmt.Println()
	},
}

func init() {
	RemovePodsCmd.Flags().IntVar(&podCount, "podCount", 1, "number of pods to remove with the same name")
}
