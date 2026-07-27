package gpu

import (
	"github.com/runpod/runpodctl/internal/api"
	"github.com/runpod/runpodctl/internal/output"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "list available gpu types",
	Long:  "list available gpu types with on-demand pricing and per-datacenter stock status",
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
	// on-demand price per hour in usd, explicitly null when that cloud type does
	// not offer the gpu. a pointer rather than a bare float64 so an agent can tell
	// "not offered" (null) from a real 0 — with omitempty the two were identical.
	SecurePricePerHr    *float64 `json:"securePricePerHr"`
	CommunityPricePerHr *float64 `json:"communityPricePerHr"`
	// StockStatus is the best availability across data centers.
	StockStatus string `json:"stockStatus,omitempty"`
	Available   bool   `json:"available"`
	// DataCenterAvailability breaks stock status down per data center.
	DataCenterAvailability []api.GpuDataCenterAvailability `json:"dataCenterAvailability,omitempty"`
}

func init() {
	listCmd.Flags().BoolVar(&includeUnavailable, "include-unavailable", false, "include gpus with no current availability")
}

// priceForCloud returns nil when the gpu is not offered on that cloud type, so
// the json carries an explicit null instead of a misleading 0.
func priceForCloud(offered bool, price float64) *float64 {
	if !offered {
		return nil
	}
	return &price
}

func runList(cmd *cobra.Command, args []string) error {
	client, err := api.NewClient()
	if err != nil {
		return err
	}

	gpus, err := client.ListGpuTypes(includeUnavailable)
	if err != nil {
		return err
	}

	typed := make([]gpuTypeOutput, 0, len(gpus))
	for _, gpu := range gpus {
		typed = append(typed, gpuTypeOutput{
			GpuID:                  gpu.ID,
			DisplayName:            gpu.DisplayName,
			MemoryInGb:             gpu.MemoryInGb,
			SecureCloud:            gpu.SecureCloud,
			CommunityCloud:         gpu.CommunityCloud,
			SecurePricePerHr:       priceForCloud(gpu.SecureCloud, gpu.SecurePrice),
			CommunityPricePerHr:    priceForCloud(gpu.CommunityCloud, gpu.CommunityPrice),
			StockStatus:            gpu.StockStatus,
			Available:              gpu.Available,
			DataCenterAvailability: gpu.DataCenterAvailability,
		})
	}

	format := output.ParseFormat(cmd.Flag("output").Value.String())
	return output.Print(typed, &output.Config{Format: format})
}
