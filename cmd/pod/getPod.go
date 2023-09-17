package pod

import (
	"cli/api"
	"cli/format"
	"fmt"
	"os"
	"strings"

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

				var portEntries int = 0
				if  p.Runtime != nil && p.Runtime.Ports != nil {
					portEntries = len(p.Runtime.Ports)
				}
				ports := make([]string, portEntries)
				for j:=0; j<portEntries; j++ {
					var privpub string = "prv"
					if p.Runtime.Ports[j].IsIpPublic {
						privpub = "pub"
					}
					ports[j] = fmt.Sprintf("%s:%d->%d\u00A0(%s,%s)",  p.Runtime.Ports[j].Ip,  p.Runtime.Ports[j].PublicPort,  p.Runtime.Ports[j].PrivatePort,  privpub,  p.Runtime.Ports[j].PortType)
				}

				row = append(
					row,
					p.PodType,
					fmt.Sprintf("%d", p.VcpuCount),
					fmt.Sprintf("%d", p.MemoryInGb),
					fmt.Sprintf("%d", p.ContainerDiskInGb),
					fmt.Sprintf("%d", p.VolumeInGb),
					fmt.Sprintf("%.3f", p.CostPerHr),
					fmt.Sprintf("%s", strings.Join(ports[:], "\n") ),
				)
			}
			data[i] = row
		}

		header := []string{"ID", "Name", "GPU", "Image Name", "Status"}
		if AllFields {
			header = append(header, "Pod Type", "vCPU", "Mem", "Container Disk", "Volume Disk", "$/hr", "Ports")
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
