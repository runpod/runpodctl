package pod

import (
	"fmt"
	"os"
	"strings"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/format"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var AllFields bool

var GetPodCmd = &cobra.Command{
	Use:   "pod [podId]",
	Args:  cobra.MaximumNArgs(1),
	Short: "get all pods",
	Long:  "get all pods or specify pod id",
	Run: func(cmd *cobra.Command, args []string) {
		pods, err := api.GetPods()
		cobra.CheckErr(err)

		data := make([][]string, len(pods))
		for i, p := range pods {
			if len(args) == 1 && p.Id != strings.ToLower(args[0]) {
				continue
			}
			row := []string{p.Id, p.Name, fmt.Sprintf("%d %s", p.GpuCount, p.Machine.GpuDisplayName), p.ImageName, p.DesiredStatus}
			if AllFields {
				row = append(
					row,
					p.PodType,
					fmt.Sprintf("%d", p.VcpuCount),
					fmt.Sprintf("%d", p.MemoryInGb),
					fmt.Sprintf("%d", p.ContainerDiskInGb),
					fmt.Sprintf("%d", p.VolumeInGb),
					fmt.Sprintf("%.3f", p.CostPerHr),
				)
			}
			data[i] = row
		}

		header := []string{"ID", "Name", "GPU", "Image Name", "Status"}
		if AllFields {
			header = append(header, "Pod Type", "vCPU", "Mem", "Container Disk", "Volume Disk", "$/hr")
		}

		tb := tablewriter.NewWriter(os.Stdout)
		tb.SetHeader(header)
		tb.AppendBulk(data)
		format.TableDefaults(tb)
		tb.Render()
	},
}

func init() {
	GetPodCmd.Flags().BoolVarP(&AllFields, "allfields", "a", false, "include all fields in output")
}
