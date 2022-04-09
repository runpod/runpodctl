package api

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

type PodOut struct {
	Data   *PodData        `json:"data"`
	Errors []*GraphQLError `json:"errors"`
}
type GraphQLError struct {
	Message string
}
type PodData struct {
	Myself *MySelfData
}
type MySelfData struct {
	Pods []*Pod
}
type Pod struct {
	Id                string
	ContainerDiskInGb int
	CostPerHr         float32
	DesiredStatus     string
	DockerArgs        string
	Env               []string
	GpuCount          int
	ImageName         string
	MemoryInGb        int
	Name              string
	PodType           string
	Ports             string
	VcpuCount         int
	VolumeInGb        int
	VolumeMountPath   string
	Machine           *Machine
}
type Machine struct {
	GpuDisplayName string
}

func QueryPods() (pods []*Pod, err error) {
	input := Input{
		Query: `
		query myPods {
			myself {
			  pods {
				id
				containerDiskInGb
				costPerHr
				desiredStatus
				dockerArgs
				dockerId
				env
				gpuCount
				imageName
				lastStatusChange
				machineId
				memoryInGb
				name
				podType
				port
				ports
				uptimeSeconds
				vcpuCount
				volumeInGb
				volumeMountPath
				machine {
				  gpuDisplayName
				}
			  }
			}
		  }
		`,
	}
	res, err := Query(input)
	if err != nil {
		err = fmt.Errorf("QueryPods: %s", err.Error())
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("QueryPods: statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		err = fmt.Errorf("QueryPods: %s", err.Error())
		return
	}
	data := &PodOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		err = fmt.Errorf("QueryPods: %s", err.Error())
		return
	}
	if len(data.Errors) > 0 {
		err = fmt.Errorf("pods query error: %s", data.Errors[0].Message)
		return
	}
	if data == nil || data.Data == nil || data.Data.Myself == nil || data.Data.Myself.Pods == nil {
		err = fmt.Errorf("QueryPods: data is nil: %s", string(rawData))
		return
	}
	pods = data.Data.Myself.Pods
	return
}
