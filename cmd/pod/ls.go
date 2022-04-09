package pod

import (
	"cli/api"
	"fmt"

	"github.com/spf13/cobra"
)

// lsCmd represents the ls command
var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "list of pods",
	Long:  `List of pods in RUNNING status.`,
	Run: func(cmd *cobra.Command, args []string) {
		pods, err := api.QueryPods()
		cobra.CheckErr(err)

		for _, p := range pods {
			fmt.Println(p.Id, p.Name, p.ImageName, p.DesiredStatus)
		}
	},
}
