package gpu

import (
	"github.com/runpod/runpod/internal/api"
	"github.com/runpod/runpod/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list available gpu types",
	Long:  "list available gpu types with stock status",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

var includeUnavailable bool

type gpuTypeOutput struct {
	GpuID          string `json:"gpuId"`
	DisplayName    string `json:"displayName"`
	MemoryInGb     int    `json:"memoryInGb"`
	SecureCloud    bool   `json:"secureCloud"`
	CommunityCloud bool   `json:"communityCloud"`
	StockStatus    string `json:"stockStatus,omitempty"`
	Available      bool   `json:"available"`
}

func init() {
	listCmd.Flags().BoolVar(&includeUnavailable, "include-unavailable", false, "include gpus with no current availability")
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		output.Error(err)
		return err
	}

	gpus, err := client.ListGpuTypes(includeUnavailable)
	if err != nil {
		output.Error(err)
		return err
	}

	typed := make([]gpuTypeOutput, 0, len(gpus))
	for _, gpu := range gpus {
		typed = append(typed, gpuTypeOutput{
			GpuID:          gpu.ID,
			DisplayName:    gpu.DisplayName,
			MemoryInGb:     gpu.MemoryInGb,
			SecureCloud:    gpu.SecureCloud,
			CommunityCloud: gpu.CommunityCloud,
			StockStatus:    gpu.StockStatus,
			Available:      gpu.Available,
		})
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(typed, &output.Config{Format: format})
}
