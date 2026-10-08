package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/runpod/runpodctl/internal/configenv"
	"github.com/spf13/viper"
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
	apiKey := configenv.APIKey()
	if apiKey == "" {
		// same typed sentinel as the rest client: every missing-key path must
		// report no_credentials, or an agent branching on that code silently
		// misses pod create, pod get, template create/update and all of ssh.
		return nil, ErrNoCredentials
	}

	apiURL := configenv.GraphQLURL()
	if apiURL == "" {
		apiURL = DefaultGraphQLURL
	}

	timeout := viper.GetDuration("graphqlTimeout")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &GraphQLClient{
		url:        apiURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		userAgent:  buildUserAgent(),
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
		return nil, parseGraphQLHTTPError(body, resp.StatusCode)
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

func sshKeyMatches(key SSHKey, name, fingerprint string) bool {
	if fingerprint != "" && key.Fingerprint != fingerprint {
		return false
	}
	if name != "" && key.Name != name {
		return false
	}
	return name != "" || fingerprint != ""
}

// PodEnvVar is a key-value pair for pod environment variables (GraphQL format)
type PodEnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LegacyPod is the graphql-shaped pod view the ssh paths and the legacy commands
// use, built from rest v2 (toLegacyPod) so their output is unchanged.
type LegacyPod struct {
	ID                string         `json:"id"`
	ContainerDiskInGb int            `json:"containerDiskInGb"`
	CostPerHr         float32        `json:"costPerHr"`
	DesiredStatus     string         `json:"desiredStatus"`
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

// LegacyMachine is the graphql-shaped machine block the legacy commands print, built from rest v2
type LegacyMachine struct {
	GpuDisplayName string `json:"gpuDisplayName"`
	Location       string `json:"location"`
}

// LegacyRuntime is a pod's runtime telemetry. there is no pulling/starting/ready
// state on it: `runtime` itself being null is the only signal that the
// container is not up yet. See internal/podstate.
type LegacyRuntime struct {
	Ports []*LegacyPort `json:"ports"`
	// UptimeInSeconds is the container's uptime.
	UptimeInSeconds *int `json:"uptimeInSeconds"`
}

// LegacyPort is one runtime port mapping.
type LegacyPort struct {
	Ip          string `json:"ip"`
	IsIpPublic  bool   `json:"isIpPublic"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort"`
	PortType    string `json:"type"`
}
