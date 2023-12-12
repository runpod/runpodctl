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

// func UpdateEndpointTemplate(endpointId string, templateId string) error {
// 	input := Input{
// 		Query: `
// 		mutation Mutation($input: UpdateEndpointTemplateInput) {
// 			updateEndpointTemplate(input: $input) {
// 			  id
// 			  templateId
// 			}
// 		  }
// 		`,
// 		Variables: map[string]interface{}{"input": UpdateEndpointTemplateInput{
// 			EndpointId: endpointId,
// 			TemplateId: templateId,
// 		}},
// 	}
// 	res, err := Query(input)
// 	if err != nil {
// 		return
// 	}
// 	defer res.Body.Close()
// 	rawData, err := io.ReadAll(res.Body)
// 	if err != nil {
// 		return
// 	}
// 	if res.StatusCode != 200 {
// 		err = fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
// 		return
// 	}
// 	data := make(map[string]interface{})
// 	if err = json.Unmarshal(rawData, &data); err != nil {
// 		return
// 	}
// 	gqlErrors, ok := data["errors"].([]interface{})
// 	if ok && len(gqlErrors) > 0 {
// 		firstErr, _ := gqlErrors[0].(map[string]interface{})
// 		err = errors.New(firstErr["message"].(string))
// 		return
// 	}
// 	gqldata, ok := data["data"].(map[string]interface{})
// 	if !ok || gqldata == nil {
// 		err = fmt.Errorf("data is nil: %s", string(rawData))
// 		return
// 	}
// 	pod, ok = gqldata["podFindAndDeployOnDemand"].(map[string]interface{})
// 	if !ok || pod == nil {
// 		err = fmt.Errorf("pod is nil: %s", string(rawData))
// 		return
// 	}
// 	return
// }
