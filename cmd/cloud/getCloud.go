package cloud

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/runpod/runpodctl/api"
	"github.com/runpod/runpodctl/format"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var (
	community bool
	disk      int
	memory    int
	vcpu      int
	secure    bool
	showDC    bool
)

var GetCloudCmd = &cobra.Command{
	Use:   "cloud [gpuCount]",
	Args:  cobra.MaximumNArgs(1),
	Short: "get all cloud gpus",
	Long:  "get all cloud gpus available on runpod.io",
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		gpuCount := 1
		if len(args) > 0 {
			gpuCount, err = strconv.Atoi(args[0])
			cobra.CheckErr(err)
			if gpuCount <= 0 {
				cobra.CheckErr(fmt.Errorf("gpu count must be > 0: %d", gpuCount))
			}
		}
		var secureCloud *bool
		if secure != community {
			secureCloud = &secure
		}
		input := &api.GetCloudInput{
			GpuCount:      gpuCount,
			MinMemoryInGb: memory,
			MinVcpuCount:  vcpu,
			SecureCloud:   secureCloud,
			TotalDisk:     disk,
		}
		gpuTypes, err := api.GetCloud(input)
		cobra.CheckErr(err)

		var dcs dcDecorator
		if showDC {
			dcs = dcDecorator{api.GetDCsByGPU()}
		}

		data := [][]string{}
		for _, gpu := range gpuTypes {
			gpuType, ok := gpu.(map[string]interface{})
			if !ok {
				continue
			}
			kv, ok := gpuType["lowestPrice"].(map[string]interface{})
			if !ok || kv["minMemory"] == nil {
				continue
			}
			spotPrice, ok := kv["minimumBidPrice"].(float64)
			spotPriceString := "Reserved"
			if ok && spotPrice > 0 {
				spotPriceString = fmt.Sprintf("%.3f", spotPrice)
			}
			onDemandPrice, ok := kv["uninterruptablePrice"].(float64)
			onDemandPriceString := "Reserved"
			if ok && spotPrice > 0 {
				onDemandPriceString = fmt.Sprintf("%.3f", onDemandPrice)
			}
			row := []string{
				fmt.Sprintf("%dx %s", gpuCount, kv["gpuTypeId"].(string)),
				fmt.Sprintf("%.f", kv["minMemory"]),
				fmt.Sprintf("%.f", kv["minVcpu"]),
				spotPriceString,
				onDemandPriceString,
			}
			row = dcs.decorateRow(row, kv["gpuTypeId"].(string))
			data = append(data, row)
		}

		header := []string{"GPU Type", "Mem GB", "vCPU", "Spot $/HR", "OnDemand $/HR"}
		header = dcs.decorateHeader(header, "Data centers")
		tb := tablewriter.NewWriter(os.Stdout)
		tb.SetHeader(header)
		tb.AppendBulk(data)
		format.TableDefaults(tb)
		tb.Render()
	},
}

type dcDecorator struct {
	dcsByGPU map[string][]string
}

func (d *dcDecorator) decorateRow(fields []string, gpuID string) []string {
	if d.dcsByGPU == nil {
		return fields
	}
	dcs, ok := d.dcsByGPU[gpuID]
	if !ok {
		return fields
	}

	return append(fields, strings.Join(dcs, ","))
}

func (d *dcDecorator) decorateHeader(headers []string, columnName string) []string {
	if d.dcsByGPU == nil {
		return headers
	}
	return append(headers, columnName)
}

func init() {
	GetCloudCmd.Flags().BoolVarP(&community, "community", "c", false, "show listings from community cloud only")
	GetCloudCmd.Flags().IntVar(&disk, "disk", 0, "minimum disk size in GB you need")
	GetCloudCmd.Flags().IntVar(&memory, "mem", 0, "minimum sys memory size in GB you need")
	GetCloudCmd.Flags().IntVar(&vcpu, "vcpu", 0, "minimum vCPUs you need")
	GetCloudCmd.Flags().BoolVarP(&secure, "secure", "s", false, "show listings from secure cloud only")
	GetCloudCmd.Flags().BoolVarP(&showDC, "datacenter", "d", false, "show which datacenters provide which gpu types")
}
