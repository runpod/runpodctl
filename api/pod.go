package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var Version string

type UserOut struct {
	Data   *PodData        `json:"data"`
	Errors []*GraphQLError `json:"errors"`
}
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
	PubKey         string
	Pods           []*Pod
	NetworkVolumes []*NetworkVolume
}
type Pod struct {
	Id                string
	ContainerDiskInGb int
	CostPerHr         float32
	DesiredStatus     string
	DataCenterId      string
	DockerArgs        string
	DockerID          string
	Env               []string
	GpuCount          int
	ImageName         string
	LastStatusChange  string
	MachineID         string
	MemoryInGb        int
	Name              string
	PodType           string
	Ports             string
	UptimeSeconds     int
	VcpuCount         int
	VolumeInGb        int
	VolumeMountPath   string
	Machine           *Machine
	Runtime           *Runtime
}
type Machine struct {
	GpuDisplayName string
	Location       string
}
type Runtime struct {
	UptimeInSeconds int
	Ports           []*Ports
	Gpus            []*Gpu
	Container       *Container
}

type Ports struct {
	Ip          string
	IsIpPublic  bool
	PrivatePort int
	PublicPort  int
	PortType    string
}

type Gpu struct {
	Id                string
	GpuUtilPercent    float32
	MemoryUtilPercent float32
}

type Container struct {
	CpuPercent    float32
	MemoryPercent float32
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
				  location
				}
				runtime {
				  uptimeInSeconds
				  ports	{
					ip
					isIpPublic
					privatePort
					publicPort
					PortType: type
				  }
				  gpus {
					id
					gpuUtilPercent
					memoryUtilPercent
				  }
				  container {
					cpuPercent
					memoryPercent
				  }
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
	rawData, err := io.ReadAll(res.Body)
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

type CreatePodInput struct {
	CloudType         string    `json:"cloudType"`
	ContainerDiskInGb int       `json:"containerDiskInGb"`
	DeployCost        float32   `json:"deployCost,omitempty"`
	DockerArgs        string    `json:"dockerArgs"`
	DataCenterId      string    `json:"dataCenterId"`
	Env               []*PodEnv `json:"env"`
	GpuCount          int       `json:"gpuCount"`
	GpuTypeId         string    `json:"gpuTypeId"`
	ImageName         string    `json:"imageName"`
	MinMemoryInGb     int       `json:"minMemoryInGb"`
	MinVcpuCount      int       `json:"minVcpuCount"`
	Name              string    `json:"name"`
	NetworkVolumeId   string    `json:"networkVolumeId"`
	Ports             string    `json:"ports"`
	SupportPublicIp   bool      `json:"supportPublicIp"`
	StartSSH          bool      `json:"startSsh"`
	TemplateId        string    `json:"templateId"`
	VolumeInGb        int       `json:"volumeInGb"`
	VolumeMountPath   string    `json:"volumeMountPath"`
}
type PodEnv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func CreatePod(podInput *CreatePodInput) (pod map[string]interface{}, err error) {
	if podInput.Name == "" {
		names := strings.Split(podInput.ImageName, ":")
		podInput.Name = names[0]
	}

	input := Input{
		Query: `
		mutation createPod($input: PodFindAndDeployOnDemandInput!) {
			podFindAndDeployOnDemand(input: $input) {
			  id
			  costPerHr
			  desiredStatus
			  lastStatusChange
			}
		}
		`,
		Variables: map[string]interface{}{"input": podInput},
	}
	res, err := Query(input)
	if err != nil {
		return
	}
	defer res.Body.Close()
	rawData, err := io.ReadAll(res.Body)
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
	pod, ok = gqldata["podFindAndDeployOnDemand"].(map[string]interface{})
	if !ok || pod == nil {
		err = fmt.Errorf("pod is nil: %s", string(rawData))
		return
	}
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
	rawData, err := io.ReadAll(res.Body)
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
	rawData, err := io.ReadAll(res.Body)
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

func StartOnDemandPod(id string) (pod map[string]interface{}, err error) {
	input := Input{
		Query: `
		mutation podResume($podId: String!) {
		  podResume(input: {podId: $podId}) {
			id
			costPerHr
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
		err = fmt.Errorf("PodBidResume: statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := io.ReadAll(res.Body)
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
	pod, ok = gqldata["podResume"].(map[string]interface{})
	if !ok || pod == nil {
		err = fmt.Errorf("pod is nil: %s", string(rawData))
		return
	}
	return
}

func StartSpotPod(id string, bidPerGpu float32, gpuCount int) (podBidResume map[string]interface{}, err error) {
	input := Input{
		Query: `
		mutation Mutation($podId: String!, $bidPerGpu: Float!, $gpuCount: Int!) {
			podBidResume(input: {podId: $podId, bidPerGpu: $bidPerGpu, gpuCount: $gpuCount}) {
			  id
			  costPerHr
			  desiredStatus
			  lastStatusChange
			}
		}
		`,
		Variables: map[string]interface{}{"podId": id, "bidPerGpu": bidPerGpu, "gpuCount": gpuCount},
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
	rawData, err := io.ReadAll(res.Body)
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
