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

// Template represents a pod template
type Template struct {
	Id                string    `json:"id"`
	Name              string    `json:"name"`
	ImageName         string    `json:"imageName"`
	DockerArgs        string    `json:"dockerArgs"`
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

// Endpoint represents a serverless endpoint
type Endpoint struct {
	Id              string    `json:"id"`
	Name            string    `json:"name"`
	TemplateId      string    `json:"templateId"`
	GpuIds          string    `json:"gpuIds"`
	GpuCount        int       `json:"gpuCount"`
	NetworkVolumeId string    `json:"networkVolumeId"`
	Locations       string    `json:"locations"`
	IdleTimeout     int       `json:"idleTimeout"`
	ScalerType      string    `json:"scalerType"`
	ScalerValue     int       `json:"scalerValue"`
	WorkersMin      int       `json:"workersMin"`
	WorkersMax      int       `json:"workersMax"`
	WorkersStandby  int       `json:"workersStandby"`
	Type            string    `json:"type"`
	Version         int       `json:"version"`
	CreatedAt       string    `json:"createdAt"`
	Env             []*PodEnv `json:"env"`
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

type TemplateOut struct {
	Data   *TemplateData   `json:"data"`
	Errors []*GraphQLError `json:"errors"`
}
type TemplateData struct {
	Myself *MySelfDataTemplate
}
type MySelfDataTemplate struct {
	PodTemplates []*Template `json:"podTemplates"`
}

type PublicTemplatesOut struct {
	Data   *PublicTemplatesData `json:"data"`
	Errors []*GraphQLError      `json:"errors"`
}

type PublicTemplatesData struct {
	Templates []*Template `json:"templates"`
}

type UpdateEndpointTemplateInput struct {
	TemplateId string `json:"templateId"`
	EndpointId string `json:"endpointId"`
}

func CreateTemplate(templateInput *CreateTemplateInput) (templateId string, err error) {
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
	defer res.Body.Close()
	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d, response: %s", res.StatusCode, string(rawData))
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

func GetTemplates() (templates []*Template, err error) {
	// Get user templates
	userTemplates, err := GetUserTemplates()
	if err != nil {
		return nil, err
	}

	// Get public templates
	publicTemplates, err := GetPublicTemplates()
	if err != nil {
		// If public templates fail, just return user templates
		return userTemplates, nil
	}

	// Combine both lists
	templates = append(userTemplates, publicTemplates...)
	return templates, nil
}

func GetUserTemplates() (templates []*Template, err error) {
	input := Input{
		Query: `
		query UserTemplates {
			myself {
			  podTemplates {
				id
				name
				imageName
				dockerArgs
				containerDiskInGb
				volumeInGb
				volumeMountPath
				ports
				env {
				  key
				  value
				}
				isServerless
				startSsh
				isPublic
				readme
			  }
			}
		  }
		`,
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
		err = fmt.Errorf("statuscode %d, response: %s", res.StatusCode, string(rawData))
		return
	}
	data := &TemplateOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		return
	}
	if len(data.Errors) > 0 {
		err = errors.New(data.Errors[0].Message)
		return
	}
	if data == nil || data.Data == nil || data.Data.Myself == nil || data.Data.Myself.PodTemplates == nil {
		templates = []*Template{} // Return empty slice instead of error
		return
	}
	templates = data.Data.Myself.PodTemplates
	return
}

func GetPublicTemplates() (templates []*Template, err error) {
	input := Input{
		Query: `
		query PublicTemplates {
			templates {
				id
				name
				imageName
				dockerArgs
				containerDiskInGb
				volumeInGb
				volumeMountPath
				ports
				env {
				  key
				  value
				}
				isServerless
				startSsh
				isPublic
				readme
			}
		}
		`,
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
		err = fmt.Errorf("statuscode %d, response: %s", res.StatusCode, string(rawData))
		return
	}
	data := &PublicTemplatesOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		return
	}
	if len(data.Errors) > 0 {
		err = errors.New(data.Errors[0].Message)
		return
	}
	if data == nil || data.Data == nil || data.Data.Templates == nil {
		templates = []*Template{} // Return empty slice instead of error
		return
	}
	templates = data.Data.Templates
	return
}

func DeleteTemplate(templateName string) (err error) {
	input := Input{
		Query: `
		mutation Mutation($templateName: String) {
			deleteTemplate(templateName: $templateName)
		}
		`,
		Variables: map[string]interface{}{"templateName": templateName},
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
		err = fmt.Errorf("statuscode %d, response: %s", res.StatusCode, string(rawData))
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
	return
}

func DeleteEndpoint(endpointId string) (err error) {
	input := Input{
		Query: `
		mutation Mutation($endpointId: String) {
			deleteEndpoint(endpointId: $endpointId)
		}
		`,
		Variables: map[string]interface{}{"endpointId": endpointId},
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
	return
}
