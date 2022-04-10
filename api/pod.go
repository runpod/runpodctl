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

func GetPods() (pods []*Pod, err error) {
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
		err = fmt.Errorf("GetPods: %s", err.Error())
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("GetPods: statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		err = fmt.Errorf("GetPods: %s", err.Error())
		return
	}
	data := &PodOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		err = fmt.Errorf("GetPods: %s", err.Error())
		return
	}
	if len(data.Errors) > 0 {
		err = fmt.Errorf("GetPods: %s", data.Errors[0].Message)
		return
	}
	if data == nil || data.Data == nil || data.Data.Myself == nil || data.Data.Myself.Pods == nil {
		err = fmt.Errorf("GetPods: data is nil: %s", string(rawData))
		return
	}
	pods = data.Data.Myself.Pods
	return
}

func PodStop(id string) (podStop map[string]interface{}, err error) {
	input := Input{
		Query: `
		mutation stopPod($podId: String!) {
		  podStop(input: {podId:  $podId}) {
			  id
			  desiredStatus
			  lastStatusChange
		    }
		  }
		`,
		Variables: map[string]interface{}{"podId": id},
	}
	res, err := Query(input)
	if err != nil {
		err = fmt.Errorf("StopPod: %s", err.Error())
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("StopPod: statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		err = fmt.Errorf("StopPod: %s", err.Error())
		return
	}
	data := make(map[string]interface{})
	if err = json.Unmarshal(rawData, &data); err != nil {
		err = fmt.Errorf("StopPod: %s", err.Error())
		return
	}
	errors, ok := data["errors"].([]interface{})
	if ok && len(errors) > 0 {
		firstErr, _ := errors[0].(map[string]interface{})
		err = fmt.Errorf("StopPod: %s", firstErr["message"])
		return
	}
	gqldata, ok := data["data"].(map[string]interface{})
	if !ok || gqldata == nil {
		err = fmt.Errorf("StopPod: data is nil: %s", string(rawData))
		return
	}
	podStop, ok = gqldata["podStop"].(map[string]interface{})
	if !ok || podStop == nil {
		err = fmt.Errorf("StopPod: podStop is nil: %s", string(rawData))
		return
	}
	return
}

func PodBidResume(id string, bidPerGpu float32) (podBidResume map[string]interface{}, err error) {
	input := Input{
		Query: `
		mutation Mutation($podId: String!, $bidPerGpu: Float!) {
			podBidResume(input: {podId: $podId, bidPerGpu: $bidPerGpu}) {
			  id
			  consumerUserId
			  desiredStatus
			  machineId
			  version
			}
		}
		`,
		Variables: map[string]interface{}{"podId": id, "bidPerGpu": bidPerGpu},
	}
	res, err := Query(input)
	if err != nil {
		err = fmt.Errorf("PodBidResume: %s", err.Error())
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("PodBidResume: statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		err = fmt.Errorf("PodBidResume: %s", err.Error())
		return
	}
	data := make(map[string]interface{})
	if err = json.Unmarshal(rawData, &data); err != nil {
		err = fmt.Errorf("PodBidResume: %s", err.Error())
		return
	}
	errors, ok := data["errors"].([]interface{})
	if ok && len(errors) > 0 {
		firstErr, _ := errors[0].(map[string]interface{})
		err = fmt.Errorf("PodBidResume: %s", firstErr["message"])
		return
	}
	gqldata, ok := data["data"].(map[string]interface{})
	if !ok || gqldata == nil {
		err = fmt.Errorf("PodBidResume: data is nil: %s", string(rawData))
		return
	}
	podBidResume, ok = gqldata["podBidResume"].(map[string]interface{})
	if !ok || podBidResume == nil {
		err = fmt.Errorf("PodBidResume: podBidResume is nil: %s", string(rawData))
		return
	}
	return
}
