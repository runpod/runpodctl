package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
)

type GetCloudInput struct {
	GpuCount      int   `json:"gpuCount"`
	MinMemoryInGb int   `json:"minMemoryInGb,omitempty"`
	MinVcpuCount  int   `json:"minVcpuCount,omitempty"`
	SecureCloud   *bool `json:"secureCloud"`
	TotalDisk     int   `json:"totalDisk,omitempty"`
}

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
	res, err := Query(input)
	if err != nil {
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
		return
	}
	data := make(map[string]interface{})
	if err = json.Unmarshal(rawData, &data); err != nil {
		return
	}
	gqlErrors, ok := data["errors"].([]interface{})
	if ok && len(gqlErrors) > 0 {
		firstErr, _ := gqlErrors[0].(map[string]interface{})
		err = errors.New(firstErr["message"].(string))
		return
	}
	gqldata, ok := data["data"].(map[string]interface{})
	if !ok || gqldata == nil {
		err = fmt.Errorf("data is nil: %s", string(rawData))
		return
	}
	gpuTypes, ok = gqldata["gpuTypes"].([]interface{})
	if !ok || gpuTypes == nil {
		err = fmt.Errorf("gpuTypes is nil: %s", string(rawData))
		return
	}
	return
}
