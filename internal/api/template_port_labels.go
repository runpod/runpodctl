package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TemplatePortLabelOverrides contains template values changed immediately
// before a portsConfig mutation. Applying them avoids reverting a fresh REST
// update when GraphQL reads are briefly stale.
type TemplatePortLabelOverrides struct {
	Name                    *string
	ImageName               *string
	IsServerless            *bool
	Ports                   *[]string
	Env                     *map[string]string
	ContainerDiskInGb       *int
	ContainerRegistryAuthID *string
	VolumeInGb              *int
	VolumeMountPath         *string
	Readme                  *string
}

var errTemplateForPortLabelsNotFound = errors.New("template not found")

const (
	templatePortLabelFetchAttempts = 10
	templatePortLabelRetryDelay    = 250 * time.Millisecond
)

type templateSaveState struct {
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	ImageName               string            `json:"imageName"`
	DockerArgs              string            `json:"dockerArgs"`
	Env                     []templateEnvPair `json:"env"`
	Ports                   templatePorts     `json:"ports"`
	VolumeMountPath         string            `json:"volumeMountPath"`
	VolumeInGb              int               `json:"volumeInGb"`
	ContainerDiskInGb       int               `json:"containerDiskInGb"`
	ContainerRegistryAuthID string            `json:"containerRegistryAuthId"`
	StartJupyter            bool              `json:"startJupyter"`
	StartSSH                bool              `json:"startSsh"`
	StartScript             string            `json:"startScript"`
	IsServerless            bool              `json:"isServerless"`
	IsPublic                bool              `json:"isPublic"`
	Readme                  string            `json:"readme"`
	AdvancedStart           bool              `json:"advancedStart"`
	Category                string            `json:"category"`
}

// UpdateTemplatePortLabels updates the dashboard labels for a template's
// exposed ports. Port labels are available through GraphQL's portsConfig
// field, but not through the public REST template schema.
func (c *GraphQLClient) UpdateTemplatePortLabels(templateID string, labels []TemplatePortConfig, overrides *TemplatePortLabelOverrides) error {
	var (
		state *templateSaveState
		err   error
	)

	for attempt := 0; attempt < templatePortLabelFetchAttempts; attempt++ {
		state, err = c.getTemplateSaveState(templateID)
		if err == nil {
			break
		}
		if !errors.Is(err, errTemplateForPortLabelsNotFound) {
			return err
		}
		if attempt+1 < templatePortLabelFetchAttempts {
			time.Sleep(templatePortLabelRetryDelay)
		}
	}
	if err != nil {
		return err
	}

	applyTemplatePortLabelOverrides(state, overrides)

	normalizedLabels, err := normalizeTemplatePortLabels(labels, state.Ports)
	if err != nil {
		return err
	}

	env := state.Env
	if env == nil {
		env = []templateEnvPair{}
	}

	input := map[string]interface{}{
		"id":                      state.ID,
		"name":                    state.Name,
		"imageName":               state.ImageName,
		"containerDiskInGb":       state.ContainerDiskInGb,
		"containerRegistryAuthId": state.ContainerRegistryAuthID,
		"dockerArgs":              state.DockerArgs,
		"env":                     env,
		"ports":                   strings.Join([]string(state.Ports), ","),
		"portsConfig":             normalizedLabels,
		"volumeInGb":              state.VolumeInGb,
		"isPublic":                state.IsPublic,
		"isServerless":            state.IsServerless,
		"startJupyter":            state.StartJupyter,
		"startSsh":                state.StartSSH,
		"advancedStart":           state.AdvancedStart,
		"readme":                  state.Readme,
	}
	if state.VolumeMountPath != "" {
		input["volumeMountPath"] = state.VolumeMountPath
	}
	if state.StartScript != "" {
		input["startScript"] = state.StartScript
	}
	if state.Category != "" {
		input["category"] = state.Category
	}

	body, err := c.Query(GraphQLInput{
		Query: `
		mutation UpdateTemplatePortLabels($input: SaveTemplateInput) {
			saveTemplate(input: $input) {
				id
			}
		}
		`,
		Variables: map[string]interface{}{"input": input},
	})
	if err != nil {
		return fmt.Errorf("failed to update template port labels: %w", err)
	}

	var response struct {
		Data struct {
			SaveTemplate *struct {
				ID string `json:"id"`
			} `json:"saveTemplate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse template port label response: %w", err)
	}
	if len(response.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", response.Errors[0].Message)
	}
	if response.Data.SaveTemplate == nil || response.Data.SaveTemplate.ID == "" {
		return errors.New("template port label update returned an empty response")
	}

	return nil
}

func (c *GraphQLClient) getTemplateSaveState(templateID string) (*templateSaveState, error) {
	body, err := c.Query(GraphQLInput{
		Query: `
		query GetTemplateForPortLabels {
			myself {
				podTemplates {
					id
					name
					imageName
					dockerArgs
					env {
						key
						value
					}
					ports
					volumeMountPath
					volumeInGb
					containerDiskInGb
					containerRegistryAuthId
					startJupyter
					startSsh
					startScript
					isServerless
					isPublic
					readme
					advancedStart
					category
				}
			}
		}
		`,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get template for port label update: %w", err)
	}

	var response struct {
		Data struct {
			Myself struct {
				PodTemplates []templateSaveState `json:"podTemplates"`
			} `json:"myself"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse template response: %w", err)
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", response.Errors[0].Message)
	}

	for i := range response.Data.Myself.PodTemplates {
		if response.Data.Myself.PodTemplates[i].ID == templateID {
			return &response.Data.Myself.PodTemplates[i], nil
		}
	}

	return nil, fmt.Errorf("%w: %s", errTemplateForPortLabelsNotFound, templateID)
}

func applyTemplatePortLabelOverrides(state *templateSaveState, overrides *TemplatePortLabelOverrides) {
	if state == nil || overrides == nil {
		return
	}
	if overrides.Name != nil {
		state.Name = *overrides.Name
	}
	if overrides.ImageName != nil {
		state.ImageName = *overrides.ImageName
	}
	if overrides.IsServerless != nil {
		state.IsServerless = *overrides.IsServerless
	}
	if overrides.Ports != nil {
		state.Ports = append(templatePorts(nil), (*overrides.Ports)...)
	}
	if overrides.Env != nil {
		state.Env = templateEnvPairs(*overrides.Env)
	}
	if overrides.ContainerDiskInGb != nil {
		state.ContainerDiskInGb = *overrides.ContainerDiskInGb
	}
	if overrides.ContainerRegistryAuthID != nil {
		state.ContainerRegistryAuthID = *overrides.ContainerRegistryAuthID
	}
	if overrides.VolumeInGb != nil {
		state.VolumeInGb = *overrides.VolumeInGb
	}
	if overrides.VolumeMountPath != nil {
		state.VolumeMountPath = *overrides.VolumeMountPath
	}
	if overrides.Readme != nil {
		state.Readme = *overrides.Readme
	}
}

func templateEnvPairs(env map[string]string) []templateEnvPair {
	if len(env) == 0 {
		return []templateEnvPair{}
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]templateEnvPair, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, templateEnvPair{Key: key, Value: env[key]})
	}
	return pairs
}

func normalizeTemplatePortLabels(labels []TemplatePortConfig, ports templatePorts) ([]TemplatePortConfig, error) {
	available := make(map[string]struct{}, len(ports))
	for _, rawPort := range ports {
		port, err := NormalizePort(rawPort)
		if err != nil {
			return nil, fmt.Errorf("template has invalid port %q: %w", rawPort, err)
		}
		available[port] = struct{}{}
	}

	if labels == nil {
		return []TemplatePortConfig{}, nil
	}

	normalized := make([]TemplatePortConfig, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		port, err := NormalizePort(label.Port)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(label.Name)
		if name == "" {
			return nil, fmt.Errorf("port label name is required for port %s", port)
		}
		if _, exists := seen[port]; exists {
			return nil, fmt.Errorf("duplicate port label for %s", port)
		}
		if _, exists := available[port]; !exists {
			return nil, fmt.Errorf("port label %s does not match any template port", port)
		}
		seen[port] = struct{}{}
		normalized = append(normalized, TemplatePortConfig{Port: port, Name: name})
	}

	return normalized, nil
}

// NormalizePort validates a port spec (optionally "port/proto" with tcp or
// http) and returns the bare port number as a string. Shared by the template
// command layer and the GraphQL port-label normalizer.
//
// The protocol is validated but then DISCARDED: "22/tcp" and "22/http" both
// normalize to "22". Port matching (labels vs. --ports) is therefore by number
// only — a protocol mismatch between --ports and --port-labels is treated as a
// match, and the label carries no protocol. This is intentional: dashboard port
// labels key on the port number.
func NormalizePort(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return "", fmt.Errorf("invalid port %q", raw)
	}
	if len(parts) == 2 {
		protocol := strings.ToLower(strings.TrimSpace(parts[1]))
		if protocol != "tcp" && protocol != "http" {
			return "", fmt.Errorf("unsupported protocol %q for port %s", protocol, strings.TrimSpace(parts[0]))
		}
	}

	port, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", fmt.Errorf("invalid port %q", raw)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("port %d is outside the valid range 1-65535", port)
	}

	return strconv.Itoa(port), nil
}
