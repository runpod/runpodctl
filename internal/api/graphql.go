package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

const (
	DefaultGraphQLURL = "https://api.runpod.io/graphql"
)

// GraphQLClient is the GraphQL API client for features not available in REST
type GraphQLClient struct {
	url        string
	apiKey     string
	httpClient *http.Client
	userAgent  string
}

// GraphQLInput is the input for a GraphQL query
type GraphQLInput struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

// NewGraphQLClient creates a new GraphQL client
func NewGraphQLClient() (*GraphQLClient, error) {
	apiKey := os.Getenv("RUNPOD_API_KEY")
	if apiKey == "" {
		apiKey = viper.GetString("apiKey")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("api key not found. run 'runpodctl config --apiKey=xxx' or set RUNPOD_API_KEY")
	}

	apiURL := os.Getenv("RUNPOD_GRAPHQL_URL")
	if apiURL == "" {
		apiURL = viper.GetString("apiUrl")
	}
	if apiURL == "" {
		apiURL = DefaultGraphQLURL
	}

	timeout := viper.GetDuration("graphqlTimeout")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	userAgent := fmt.Sprintf("runpod-cli/%s (%s; %s)", Version, runtime.GOOS, runtime.GOARCH)

	return &GraphQLClient{
		url:        apiURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  userAgent,
	}, nil
}

// Query executes a GraphQL query
func (c *GraphQLClient) Query(input GraphQLInput) ([]byte, error) {
	if input.Variables == nil {
		input.Variables = map[string]interface{}{}
	}

	jsonValue, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.url, bytes.NewBuffer(jsonValue))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("graphql error: status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// SSHKey represents an SSH key
type SSHKey struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

// GetPublicSSHKeys gets the user's SSH keys via GraphQL
func (c *GraphQLClient) GetPublicSSHKeys() (string, []SSHKey, error) {
	input := GraphQLInput{
		Query: `
		query myself {
			myself {
				id
				pubKey
			}
		}
		`,
	}

	body, err := c.Query(input)
	if err != nil {
		return "", nil, err
	}

	var data struct {
		Data struct {
			Myself struct {
				PubKey string `json:"pubKey"`
			} `json:"myself"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return "", nil, err
	}

	if len(data.Errors) > 0 {
		return "", nil, fmt.Errorf("graphql error: %s", data.Errors[0].Message)
	}

	// Parse the public key string into a list of SSHKey structs
	var keys []SSHKey
	keyStrings := strings.Split(data.Data.Myself.PubKey, "\n")
	for _, keyString := range keyStrings {
		if keyString == "" {
			continue
		}

		pubKey, name, _, _, err := ssh.ParseAuthorizedKey([]byte(keyString))
		if err != nil {
			continue // Skip keys that can't be parsed
		}

		keys = append(keys, SSHKey{
			Name:        name,
			Type:        pubKey.Type(),
			Key:         string(ssh.MarshalAuthorizedKey(pubKey)),
			Fingerprint: ssh.FingerprintSHA256(pubKey),
		})
	}

	return data.Data.Myself.PubKey, keys, nil
}

// AddPublicSSHKey adds an SSH key via GraphQL
func (c *GraphQLClient) AddPublicSSHKey(key []byte) error {
	rawKeys, existingKeys, err := c.GetPublicSSHKeys()
	if err != nil {
		return fmt.Errorf("failed to get existing SSH keys: %w", err)
	}

	keyStr := string(key)
	for _, k := range existingKeys {
		if strings.TrimSpace(k.Key) == strings.TrimSpace(keyStr) {
			return nil
		}
	}

	// Concatenate the new key onto the existing keys, separated by a newline
	newKeys := strings.TrimSpace(rawKeys)
	if newKeys != "" {
		newKeys += "\n\n"
	}
	newKeys += strings.TrimSpace(keyStr)

	input := GraphQLInput{
		Query: `
		mutation Mutation($input: UpdateUserSettingsInput) {
			updateUserSettings(input: $input) {
			  id
			}
		  }
		`,
		Variables: map[string]interface{}{"input": map[string]interface{}{"pubKey": newKeys}},
	}

	if _, err = c.Query(input); err != nil {
		return fmt.Errorf("failed to update SSH keys: %w", err)
	}

	return nil
}

// PodEnvVar is a key-value pair for pod environment variables (GraphQL format)
type PodEnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreatePodGQLInput is the input for creating a pod via GraphQL
type CreatePodGQLInput struct {
	CloudType         string       `json:"cloudType,omitempty"`
	ContainerDiskInGb int          `json:"containerDiskInGb"`
	DataCenterId      string       `json:"dataCenterId,omitempty"`
	Env               []*PodEnvVar `json:"env,omitempty"`
	GpuCount          int          `json:"gpuCount"`
	GpuTypeId         string       `json:"gpuTypeId,omitempty"`
	ImageName         string       `json:"imageName,omitempty"`
	Name              string       `json:"name,omitempty"`
	Ports             string       `json:"ports,omitempty"`
	StartSsh          bool         `json:"startSsh"`
	SupportPublicIp   bool         `json:"supportPublicIp,omitempty"`
	TemplateId        string       `json:"templateId,omitempty"`
	VolumeInGb        int          `json:"volumeInGb,omitempty"`
	VolumeMountPath   string       `json:"volumeMountPath,omitempty"`
	NetworkVolumeId   string       `json:"networkVolumeId,omitempty"`
	MinCudaVersion         string       `json:"minCudaVersion,omitempty"`
	DockerArgs             string       `json:"dockerArgs,omitempty"`
	ContainerRegistryAuthId string     `json:"containerRegistryAuthId,omitempty"`
	CountryCode            string       `json:"countryCode,omitempty"`
	StopAfter              string       `json:"stopAfter,omitempty"`
	TerminateAfter         string       `json:"terminateAfter,omitempty"`
	Compliance             []string     `json:"compliance,omitempty"`
}

// CreatePod creates a pod via GraphQL (podFindAndDeployOnDemand)
func (c *GraphQLClient) CreatePod(input *CreatePodGQLInput) (map[string]interface{}, error) {
	gqlInput := GraphQLInput{
		Query: `
		mutation createPod($input: PodFindAndDeployOnDemandInput!) {
			podFindAndDeployOnDemand(input: $input) {
				id
				name
				imageName
				desiredStatus
				costPerHr
				containerDiskInGb
				volumeInGb
				volumeMountPath
				gpuCount
				memoryInGb
				vcpuCount
				ports
				lastStatusChange
				env
				machine {
					gpuDisplayName
					location
				}
			}
		}
		`,
		Variables: map[string]interface{}{"input": input},
	}

	body, err := c.Query(gqlInput)
	if err != nil {
		return nil, err
	}

	var data struct {
		Data struct {
			Pod map[string]interface{} `json:"podFindAndDeployOnDemand"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	if len(data.Errors) > 0 {
		return nil, fmt.Errorf("%s", data.Errors[0].Message)
	}

	if data.Data.Pod == nil {
		return nil, fmt.Errorf("pod creation returned nil response")
	}

	return data.Data.Pod, nil
}

// LegacyPod is the pod structure from GraphQL API (for backwards compatibility)
type LegacyPod struct {
	ID                string         `json:"id"`
	ContainerDiskInGb int            `json:"containerDiskInGb"`
	CostPerHr         float32        `json:"costPerHr"`
	DesiredStatus     string         `json:"desiredStatus"`
	LastStatusChange  interface{}    `json:"lastStatusChange,omitempty"`
	UptimeSeconds     interface{}    `json:"uptimeSeconds,omitempty"`
	DockerArgs        string         `json:"dockerArgs"`
	Env               []string       `json:"env"`
	GpuCount          int            `json:"gpuCount"`
	ImageName         string         `json:"imageName"`
	MemoryInGb        int            `json:"memoryInGb"`
	Name              string         `json:"name"`
	PodType           string         `json:"podType"`
	Ports             string         `json:"ports"`
	VcpuCount         int            `json:"vcpuCount"`
	VolumeInGb        int            `json:"volumeInGb"`
	VolumeMountPath   string         `json:"volumeMountPath"`
	Machine           *LegacyMachine `json:"machine"`
	Runtime           *LegacyRuntime `json:"runtime"`
}

// LegacyMachine is the machine structure from GraphQL API
type LegacyMachine struct {
	GpuDisplayName string `json:"gpuDisplayName"`
	Location       string `json:"location"`
}

// LegacyRuntime is the runtime structure from GraphQL API
type LegacyRuntime struct {
	Ports []*LegacyPort `json:"ports"`
}

// LegacyPort is the port structure from GraphQL API
type LegacyPort struct {
	Ip          string `json:"ip"`
	IsIpPublic  bool   `json:"isIpPublic"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort"`
	PortType    string `json:"type"`
}

// GetPods gets pods via GraphQL (for ssh connect which needs runtime info)
func (c *GraphQLClient) GetPods() ([]*LegacyPod, error) {
	input := GraphQLInput{
		Query: `
		query myPods {
			myself {
			  pods {
				id
				containerDiskInGb
				costPerHr
				desiredStatus
				lastStatusChange
				uptimeSeconds
				dockerArgs
				env
				gpuCount
				imageName
				memoryInGb
				name
				podType
				ports
				vcpuCount
				volumeInGb
				volumeMountPath
				machine {
				  gpuDisplayName
				  location
				}
				runtime {
				  ports {
					ip
					isIpPublic
					privatePort
					publicPort
					type
				  }
				}
			  }
			}
		}
		`,
	}

	body, err := c.Query(input)
	if err != nil {
		return nil, err
	}

	var data struct {
		Data struct {
			Myself struct {
				Pods []*LegacyPod `json:"pods"`
			} `json:"myself"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	if len(data.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", data.Errors[0].Message)
	}

	return data.Data.Myself.Pods, nil
}

// LegacyNetworkVolume is the network volume structure from GraphQL API
type LegacyNetworkVolume struct {
	ID           string `json:"id"`
	DataCenterID string `json:"dataCenterId"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
}

// GetNetworkVolumes gets network volumes via GraphQL
func (c *GraphQLClient) GetNetworkVolumes() ([]*LegacyNetworkVolume, error) {
	input := GraphQLInput{
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

	body, err := c.Query(input)
	if err != nil {
		return nil, err
	}

	var data struct {
		Data struct {
			Myself struct {
				NetworkVolumes []*LegacyNetworkVolume `json:"networkVolumes"`
			} `json:"myself"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	if len(data.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", data.Errors[0].Message)
	}

	return data.Data.Myself.NetworkVolumes, nil
}
