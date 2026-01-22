package pod

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/format"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var AllFields bool
var outputJSON bool

var GetPodCmd = &cobra.Command{
	Use:   "pod [podId]",
	Args:  cobra.MaximumNArgs(1),
	Short: "get all pods",
	Long:  "get all pods or specify pod id",
	Run: func(cmd *cobra.Command, args []string) {
		pods, err := api.GetPods()
		cobra.CheckErr(err)

		pods = filter(pods, args)

		if outputJSON {
			cobra.CheckErr(toJSON(os.Stdout, pods))
		} else {
			toTable(pods)
		}
	},
}

func init() {
	GetPodCmd.Flags().BoolVarP(&AllFields, "allfields", "a", false, "include all fields in output")
	GetPodCmd.Flags().BoolVarP(&outputJSON, "json", "j", false, "use json as output format")
}

func filter(pods []*api.Pod, args []string) []*api.Pod {
	filtered := make([]*api.Pod, 0) // ensures [] instead of "null" if no pods when mashalling
	for _, p := range pods {
		if len(args) == 1 && p.Id != strings.ToLower(args[0]) {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func toTable(pods []*api.Pod) {
	data := make([][]string, len(pods))
	for i, p := range pods {
		row := []string{p.Id, p.Name, fmt.Sprintf("%d %s", p.GpuCount, p.Machine.GpuDisplayName), p.ImageName, p.DesiredStatus}
		if AllFields {

			var portEntries int = 0
			if p.Runtime != nil && p.Runtime.Ports != nil {
				portEntries = len(p.Runtime.Ports)
			}
			ports := make([]string, portEntries)
			for j := range ports {
				var privpub string = "prv"
				if p.Runtime.Ports[j].IsIpPublic {
					privpub = "pub"
				}
				ports[j] = fmt.Sprintf("%s:%d->%d\u00A0(%s,%s)", p.Runtime.Ports[j].Ip, p.Runtime.Ports[j].PublicPort, p.Runtime.Ports[j].PrivatePort, privpub, p.Runtime.Ports[j].PortType)
			}

			row = append(
				row,
				p.PodType,
				fmt.Sprintf("%d", p.VcpuCount),
				fmt.Sprintf("%d", p.MemoryInGb),
				fmt.Sprintf("%d", p.ContainerDiskInGb),
				fmt.Sprintf("%d", p.VolumeInGb),
				fmt.Sprintf("%s", p.Machine.Location),
				fmt.Sprintf("%.3f", p.CostPerHr),
				fmt.Sprintf("%s", strings.Join(ports[:], ",")),
			)
		}
		data[i] = row
	}

	header := []string{"ID", "Name", "GPU", "Image Name", "Status"}
	if AllFields {
		header = append(header, "Pod Type", "vCPU", "Mem", "Container Disk", "Volume Disk", "Location", "$/hr", "Ports")
	}

	tb := tablewriter.NewWriter(os.Stdout)
	tb.SetHeader(header)
	tb.AppendBulk(data)
	format.TableDefaults(tb)
	tb.Render()
}

func toJSON(w io.Writer, pods []*api.Pod) error {
	b, err := json.MarshalIndent(pods, "", "  ")
	if err != nil {
		return err
	}

	_, err = w.Write(b)
	return err
}
