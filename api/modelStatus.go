package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PodModelVersion is one model version a pod (worker) requires, with the
// machine-level assignment diagnostics reported by the host (STO-359/STO-529).
// FailurePhase/FailureReason are only populated when Status is "FAILED";
// MountPath is populated whenever the host has reported it, on any status.
// All four diagnostics fields are nil when no assignment row exists yet --
// i.e. the model has not been tracked on this worker's machine.
type PodModelVersion struct {
	ModelVersionUUID string  `json:"modelVersionUuid"`
	ModelVersionHash string  `json:"modelVersionHash"`
	Status           *string `json:"machineAssignmentStatus"`
	FailurePhase     *string `json:"failurePhase"`
	FailureReason    *string `json:"failureReason"`
	MountPath        *string `json:"mountPath"`
}

// GetEndpointModelReferences returns the model references configured on an
// endpoint (e.g. "https://huggingface.co/org/model:revision" or a Model Repo
// reference) -- REST omits this field entirely, so it must go through
// GraphQL. Returns an empty slice, not an error, for an endpoint with no
// configured model references.
func GetEndpointModelReferences(endpointID string) ([]string, error) {
	id := strings.TrimSpace(endpointID)
	if id == "" {
		return nil, fmt.Errorf("endpointID cannot be empty")
	}

	gqlInput := Input{
		Query: `
                query EndpointModelReferences($id: String!) {
                        myself {
                                endpoint(id: $id) {
                                        modelReferences
                                }
                        }
                }
                `,
		Variables: map[string]interface{}{"id": id},
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
	}

	var data struct {
		Data *struct {
			Myself *struct {
				Endpoint *struct {
					ModelReferences []string `json:"modelReferences"`
				} `json:"endpoint"`
			} `json:"myself"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, errors.New(data.Errors[0].Message)
	}
	if data.Data == nil || data.Data.Myself == nil || data.Data.Myself.Endpoint == nil {
		return nil, fmt.Errorf("endpoint %s not found", id)
	}

	return data.Data.Myself.Endpoint.ModelReferences, nil
}

// GetPodModelVersions returns the model versions a pod (worker) requires,
// each with the machine-level assignment diagnostics reported by the host.
// Returns an empty slice, not an error, for a pod with no model dependency.
func GetPodModelVersions(podID string) ([]*PodModelVersion, error) {
	id := strings.TrimSpace(podID)
	if id == "" {
		return nil, fmt.Errorf("podID cannot be empty")
	}

	gqlInput := Input{
		Query: `
                query PodModelVersions($podId: String!) {
                        pod(input: { podId: $podId }) {
                                modelVersions {
                                        modelVersionUuid
                                        modelVersionHash
                                        machineAssignmentStatus
                                        failurePhase
                                        failureReason
                                        mountPath
                                }
                        }
                }
                `,
		Variables: map[string]interface{}{"podId": id},
	}

	res, err := Query(gqlInput)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("statuscode %d: %s", res.StatusCode, string(rawData))
	}

	var data struct {
		Data *struct {
			Pod *struct {
				ModelVersions []*PodModelVersion `json:"modelVersions"`
			} `json:"pod"`
		} `json:"data"`
		Errors []*GraphQLError `json:"errors"`
	}
	if err = json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	if len(data.Errors) > 0 {
		return nil, errors.New(data.Errors[0].Message)
	}
	if data.Data == nil || data.Data.Pod == nil {
		return nil, fmt.Errorf("pod %s not found", id)
	}

	return data.Data.Pod.ModelVersions, nil
}
