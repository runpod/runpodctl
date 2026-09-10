package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// EndpointEnvVar is one key/value pair from an endpoint's own env override
// (GraphQL Endpoint.env), which is what reaches the running container -- not
// the template's default env that `runpodctl template get` returns.
type EndpointEnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// GetEndpointEnvOverride returns an endpoint's own env override. REST omits
// the field entirely, so this must go through GraphQL. Returns an empty
// slice, not an error, when no override is configured.
func GetEndpointEnvOverride(endpointID string) ([]EndpointEnvVar, error) {
	id := strings.TrimSpace(endpointID)
	if id == "" {
		return nil, fmt.Errorf("endpointID cannot be empty")
	}

	gqlInput := Input{
		Query: `
                query EndpointEnvOverride($id: String!) {
                        myself {
                                endpoint(id: $id) {
                                        env {
                                                key
                                                value
                                        }
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
					Env []EndpointEnvVar `json:"env"`
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

	env := data.Data.Myself.Endpoint.Env
	if env == nil {
		env = []EndpointEnvVar{}
	}
	return env, nil
}
