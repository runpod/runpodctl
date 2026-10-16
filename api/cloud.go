package api

import (
	"fmt"
)

type GetCloudInput struct {
	GpuCount      int   `json:"gpuCount"`
	MinMemoryInGb int   `json:"minMemoryInGb,omitempty"`
	MinVcpuCount  int   `json:"minVcpuCount,omitempty"`
	SecureCloud   *bool `json:"secureCloud"`
	TotalDisk     int   `json:"totalDisk,omitempty"`
}

// rp-migrate: keep-v1 -- the v2 catalog has no spot price and no per-count
// minimum memory or vcpu, which is what this table prints.
func GetCloud(in *GetCloudInput) (gpuTypes []interface{}, err error) {
	input := Input{
		Query: `
		query LowestPrice($input: GpuLowestPriceInput!) {
			gpuTypes {
			  lowestPrice(input: $input) {
				gpuName
				gpuTypeId
				minimumBidPrice
				uninterruptablePrice
				minMemory
				minVcpu
			  }
			}
		}
		`,
		Variables: map[string]interface{}{"input": in},
	}
	gqldata, rawData, err := queryGraphQLData(input)
	if err != nil {
		return
	}
	gpuTypes, ok := gqldata["gpuTypes"].([]interface{})
	if !ok || gpuTypes == nil {
		err = fmt.Errorf("gpuTypes is nil: %s", string(rawData))
		return
	}
	return
}
