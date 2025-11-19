package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type NetworkVolume struct {
	Id           string `json:"id"`
	DataCenterId string `json:"dataCenterId"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
}

func GetNetworkVolumes() (volumes []*NetworkVolume, err error) {
	input := Input{
		Query: `
		query getNetworkVolumes {
			myself {
			  networkVolumes {
				dataCenterId
				id
				name
				size
			  }
			}
		}
		`,
	}
	res, err := Query(input)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	data := &PodOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		err = errors.New(data.Errors[0].Message)
		return nil, err
	}
	if data.Data == nil || data.Data.Myself == nil || data.Data.Myself.NetworkVolumes == nil {
		err = fmt.Errorf("data is nil: %s", string(rawData))
		return nil, err
	}
	return data.Data.Myself.NetworkVolumes, nil
}
