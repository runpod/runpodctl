package api

import (
	"encoding/json"
	"errors"
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
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}
	data := &PodOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		return
	}
	if len(data.Errors) > 0 {
		err = errors.New(data.Errors[0].Message)
		return
	}
	if data == nil || data.Data == nil || data.Data.Myself == nil || data.Data.Myself.Pods == nil {
		err = fmt.Errorf("data is nil: %s", string(rawData))
		return
	}
	pods = data.Data.Myself.Pods
	return
}

func StopPod(id string) (podStop map[string]interface{}, err error) {
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
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
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
	podStop, ok = gqldata["podStop"].(map[string]interface{})
	if !ok || podStop == nil {
		err = fmt.Errorf("podStop is nil: %s", string(rawData))
		return
	}
	return
}

func RemovePod(id string) (ok bool, err error) {
	input := Input{
		Query: `
		mutation terminatePod($podId: String!) {
		  podTerminate(input: {podId:  $podId})
		}
		`,
		Variables: map[string]interface{}{"podId": id},
	}
	res, err := Query(input)
	if err != nil {
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
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
	_, ok = gqldata["podTerminate"]
	return
}

func StartSpotPod(id string, bidPerGpu float32) (podBidResume map[string]interface{}, err error) {
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
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("PodBidResume: statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := ioutil.ReadAll(res.Body)
	if err != nil {
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
	podBidResume, ok = gqldata["podBidResume"].(map[string]interface{})
	if !ok || podBidResume == nil {
		err = fmt.Errorf("podBidResume is nil: %s", string(rawData))
		return
	}
	return
}
