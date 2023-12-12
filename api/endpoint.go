package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type CreateTemplateInput struct {
	Name              string    `json:"name"`
	ImageName         string    `json:"imageName"`
	DockerStartCmd    string    `json:"dockerArgs"`
	ContainerDiskInGb int       `json:"containerDiskInGb"`
	VolumeInGb        int       `json:"volumeInGb"`
	VolumeMountPath   string    `json:"volumeMountPath"`
	Ports             string    `json:"ports"`
	Env               []*PodEnv `json:"env"`
	IsServerless      bool      `json:"isServerless"`
	StartSSH          bool      `json:"startSsh"`
	IsPublic          bool      `json:"isPublic"`
	Readme            string    `json:"readme"`
}
type CreateEndpointInput struct {
	Name            string `json:"name"`
	TemplateId      string `json:"templateId"`
	GpuIds          string `json:"gpuIds"`
	NetworkVolumeId string `json:"networkVolumeId"`
	Locations       string `json:"locations"`
	IdleTimeout     int    `json:"idleTimeout"`
	ScalerType      string `json:"scalerType"`
	ScalerValue     int    `json:"scalerValue"`
	WorkersMin      int    `json:"workersMin"`
	WorkersMax      int    `json:"workersMax"`
}

// there are many more fields in the result of the query but I just care about these for CLI port
type Endpoint struct {
	Name string `json:"name"`
	Id   string
}
type EndpointOut struct {
	Data   *EndpointData   `json:"data"`
	Errors []*GraphQLError `json:"errors"`
}
type EndpointData struct {
	Myself *MySelfDataEndpoint
}
type MySelfDataEndpoint struct {
	Endpoints []*Endpoint
}

type UpdateEndpointTemplateInput struct {
	TemplateId string `json:"templateId"`
	EndpointId string `json:"endpointId"`
}

func CreateTemplate(templateInput *CreateTemplateInput) (templateId string, err error) {
	inputJson, err := json.Marshal(templateInput)
	fmt.Println(string(inputJson))
	input := Input{
		Query: `
		mutation saveTemplate($input: SaveTemplateInput) {
			saveTemplate(input: $input) {
			  advancedStart
			  containerDiskInGb
			  dockerArgs
			  env {
				key
				value
			  }
			  id
			  imageName
			  name
			  ports
			  readme
			  startJupyter
			  startScript
			  startSsh
			  volumeInGb
			  volumeMountPath
			}
		  }
		`,
		Variables: map[string]interface{}{"input": templateInput},
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
	template, ok := gqldata["saveTemplate"].(map[string]interface{})
	if !ok || template == nil {
		err = fmt.Errorf("template is nil: %s", string(rawData))
		return
	}
	templateId = template["id"].(string)
	return
}

func CreateEndpoint(endpointInput *CreateEndpointInput) (endpointId string, err error) {
	input := Input{
		Query: `
		mutation saveEndpoint($input: EndpointInput!) {
			saveEndpoint(input: $input) {
			  gpuIds
			  id
			  idleTimeout
			  locations
			  name
			  networkVolumeId
			  scalerType
			  scalerValue
			  templateId
			  userId
			  workersMax
			  workersMin
			}
		  }
		`,
		Variables: map[string]interface{}{"input": endpointInput},
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
	endpoint, ok := gqldata["saveEndpoint"].(map[string]interface{})
	if !ok || endpoint == nil {
		err = fmt.Errorf("endpoint is nil: %s", string(rawData))
		return
	}
	endpointId = endpoint["id"].(string)
	return
}

func UpdateEndpointTemplate(endpointId string, templateId string) (err error) {
	input := Input{
		Query: `
		mutation Mutation($input: UpdateEndpointTemplateInput) {
			updateEndpointTemplate(input: $input) {
			  id
			  templateId
			}
		  }
		`,
		Variables: map[string]interface{}{"input": UpdateEndpointTemplateInput{
			EndpointId: endpointId,
			TemplateId: templateId,
		}},
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
	return
}

func GetEndpoints() (endpoints []*Endpoint, err error) {
	input := Input{
		Query: `
		query Query {
			myself {
			  endpoints {
				aiKey
				gpuIds
				id
				idleTimeout
				name
				networkVolumeId
				locations
				scalerType
				scalerValue
				templateId
				type
				userId
				version
				workersMax
				workersMin
				workersStandby
				gpuCount
				env {
				  key
				  value
				}
				createdAt
				networkVolume {
				  id
				  dataCenterId
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
	data := &EndpointOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		return
	}
	if len(data.Errors) > 0 {
		err = errors.New(data.Errors[0].Message)
		return
	}
	if data == nil || data.Data == nil || data.Data.Myself == nil || data.Data.Myself.Endpoints == nil {
		err = fmt.Errorf("data is nil: %s", string(rawData))
		return
	}
	endpoints = data.Data.Myself.Endpoints
	return
}
