package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Template represents a runpod template
type Template struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	ImageName         string            `json:"imageName"`
	IsServerless      bool              `json:"isServerless,omitempty"`
	IsPublic          bool              `json:"isPublic,omitempty"`
	IsRunpod          bool              `json:"isRunpod,omitempty"`
	Category          string            `json:"category,omitempty"`
	Ports             []string          `json:"ports,omitempty"`
	DockerEntrypoint  []string          `json:"dockerEntrypoint,omitempty"`
	DockerStartCmd    []string          `json:"dockerStartCmd,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	ContainerDiskInGb int               `json:"containerDiskInGb,omitempty"`
	VolumeInGb        int               `json:"volumeInGb,omitempty"`
	VolumeMountPath   string            `json:"volumeMountPath,omitempty"`
	Readme            string            `json:"readme,omitempty"`
}

type templateEnvPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type templatePorts []string

func (p *templatePorts) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		ports := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			ports = append(ports, part)
		}
		*p = ports
		return nil
	}

	var ports []string
	if err := json.Unmarshal(data, &ports); err != nil {
		return err
	}
	*p = ports
	return nil
}

type templateGraphQL struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	ImageName         string            `json:"imageName"`
	IsServerless      bool              `json:"isServerless,omitempty"`
	IsPublic          bool              `json:"isPublic,omitempty"`
	IsRunpod          bool              `json:"isRunpod,omitempty"`
	Category          string            `json:"category,omitempty"`
	Ports             templatePorts     `json:"ports,omitempty"`
	Env               []templateEnvPair `json:"env,omitempty"`
	ContainerDiskInGb int               `json:"containerDiskInGb,omitempty"`
	VolumeInGb        int               `json:"volumeInGb,omitempty"`
	VolumeMountPath   string            `json:"volumeMountPath,omitempty"`
	Readme            string            `json:"readme,omitempty"`
}

// TemplateType for filtering
type TemplateType string

const (
	TemplateTypeAll       TemplateType = "all"
	TemplateTypeOfficial  TemplateType = "official"
	TemplateTypeCommunity TemplateType = "community"
	TemplateTypeUser      TemplateType = "user"
)

// TemplateListOptions for listing templates
type TemplateListOptions struct {
	Type   TemplateType
	Search string // search term to filter by name/image
	Limit  int
	Offset int
}

// TemplateListResponse is the response from listing templates
type TemplateListResponse struct {
	Templates []Template `json:"templates"`
}

// TemplateCreateRequest is the request to create a template
type TemplateCreateRequest struct {
	Name              string            `json:"name"`
	ImageName         string            `json:"imageName"`
	IsServerless      bool              `json:"isServerless,omitempty"`
	Ports             []string          `json:"ports,omitempty"`
	DockerEntrypoint  []string          `json:"dockerEntrypoint,omitempty"`
	DockerStartCmd    []string          `json:"dockerStartCmd,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	ContainerDiskInGb int               `json:"containerDiskInGb,omitempty"`
	VolumeInGb        int               `json:"volumeInGb,omitempty"`
	VolumeMountPath   string            `json:"volumeMountPath,omitempty"`
	Readme            string            `json:"readme,omitempty"`
}

// TemplateUpdateRequest is the request to update a template
type TemplateUpdateRequest struct {
	Name              string            `json:"name,omitempty"`
	ImageName         string            `json:"imageName,omitempty"`
	Ports             []string          `json:"ports,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	Readme            string            `json:"readme,omitempty"`
	ContainerDiskInGb *int              `json:"containerDiskInGb,omitempty"`
}

// ListTemplates returns templates (user's own via REST API)
func (c *Client) ListTemplates() ([]Template, error) {
	data, err := c.Get("/templates", nil)
	if err != nil {
		return nil, err
	}

	var templates []Template
	if err := json.Unmarshal(data, &templates); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return templates, nil
}

// ListAllTemplates returns templates based on filter options
// Uses GraphQL podTemplates(input:) for official/community, REST API for user templates
//
// Default behavior (no type specified): official + community templates
// --type official: only RunPod official templates
// --type community: only community templates
// --type user: only user's own templates
// --all: everything including user templates
func (c *Client) ListAllTemplates(opts *TemplateListOptions) ([]Template, error) {
	query := `
		query PodTemplates($input: PodTemplateInput) {
			podTemplates(input: $input) {
				id
				name
				imageName
				isServerless
				isPublic
				isRunpod
				category
				containerDiskInGb
				volumeInGb
				volumeMountPath
			}
		}
	`

	var allTemplates []Template

	// Determine what to fetch based on type filter
	// Default (no type): official + community (NOT user - they need to explicitly ask)
	fetchOfficial := opts == nil || opts.Type == "" || opts.Type == TemplateTypeAll || opts.Type == TemplateTypeOfficial
	fetchCommunity := opts == nil || opts.Type == "" || opts.Type == TemplateTypeAll || opts.Type == TemplateTypeCommunity
	fetchUser := opts != nil && (opts.Type == TemplateTypeAll || opts.Type == TemplateTypeUser)

	// Fetch official RunPod templates (isRunpod: true) FIRST
	if fetchOfficial && (opts == nil || opts.Type != TemplateTypeCommunity) {
		variables := map[string]interface{}{
			"input": map[string]interface{}{
				"isRunpod": true,
			},
		}
		data, err := c.graphqlRequest(query, variables)
		if err == nil {
			var resp struct {
				Data struct {
					PodTemplates []Template `json:"podTemplates"`
				} `json:"data"`
			}
			if json.Unmarshal(data, &resp) == nil {
				allTemplates = append(allTemplates, resp.Data.PodTemplates...)
			}
		}
	}

	// Fetch community templates (isRunpod: false) SECOND
	if fetchCommunity && (opts == nil || opts.Type != TemplateTypeOfficial) {
		variables := map[string]interface{}{
			"input": map[string]interface{}{
				"isRunpod": false,
			},
		}
		data, err := c.graphqlRequest(query, variables)
		if err == nil {
			var resp struct {
				Data struct {
					PodTemplates []Template `json:"podTemplates"`
				} `json:"data"`
			}
			if json.Unmarshal(data, &resp) == nil {
				allTemplates = append(allTemplates, resp.Data.PodTemplates...)
			}
		}
	}

	// Fetch user's own templates via REST API LAST
	if fetchUser {
		userTemplates, err := c.ListTemplates()
		if err == nil {
			allTemplates = append(allTemplates, userTemplates...)
		}
	}

	// Apply search filter (client-side, matching runpod-assistant behavior)
	if opts != nil && opts.Search != "" {
		searchTerm := strings.ToLower(opts.Search)
		var filtered []Template
		for _, t := range allTemplates {
			if strings.Contains(strings.ToLower(t.ID), searchTerm) ||
				strings.Contains(strings.ToLower(t.Name), searchTerm) ||
				strings.Contains(strings.ToLower(t.ImageName), searchTerm) {
				filtered = append(filtered, t)
			}
		}
		allTemplates = filtered
	}

	// Apply pagination
	if opts != nil {
		if opts.Offset > 0 && opts.Offset < len(allTemplates) {
			allTemplates = allTemplates[opts.Offset:]
		}
		if opts.Limit > 0 && opts.Limit < len(allTemplates) {
			allTemplates = allTemplates[:opts.Limit]
		}
	}

	return allTemplates, nil
}

// GetTemplate returns a single template by ID
// First tries REST API (user templates), then falls back to GraphQL for any template
func (c *Client) GetTemplate(templateID string) (*Template, error) {
	// Try REST API first (works for user's own templates)
	data, err := c.Get("/templates/"+templateID, nil)
	if err == nil {
		var template Template
		if err := json.Unmarshal(data, &template); err == nil {
			return &template, nil
		}
	}

	// Fall back to GraphQL for official/public templates
	return c.getTemplateByIDGraphQL(templateID)
}

// getTemplateByIDGraphQL retrieves a template by ID using GraphQL
func (c *Client) getTemplateByIDGraphQL(templateID string) (*Template, error) {
	query := `
		query GetTemplate($id: String!) {
			podTemplate(id: $id) {
				id
				name
				imageName
				isServerless
				isPublic
				isRunpod
				category
				ports
				env {
					key
					value
				}
				containerDiskInGb
				volumeInGb
				volumeMountPath
				readme
			}
		}
	`

	variables := map[string]interface{}{
		"id": templateID,
	}

	data, err := c.graphqlRequest(query, variables)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			PodTemplate *templateGraphQL `json:"podTemplate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	if resp.Data.PodTemplate == nil {
		return nil, fmt.Errorf("template not found: %s", templateID)
	}

	return templateFromGraphQL(resp.Data.PodTemplate), nil
}

func templateFromGraphQL(source *templateGraphQL) *Template {
	if source == nil {
		return nil
	}

	template := &Template{
		ID:                source.ID,
		Name:              source.Name,
		ImageName:         source.ImageName,
		IsServerless:      source.IsServerless,
		IsPublic:          source.IsPublic,
		IsRunpod:          source.IsRunpod,
		Category:          source.Category,
		Ports:             []string(source.Ports),
		ContainerDiskInGb: source.ContainerDiskInGb,
		VolumeInGb:        source.VolumeInGb,
		VolumeMountPath:   source.VolumeMountPath,
		Readme:            source.Readme,
	}

	if len(source.Env) > 0 {
		env := make(map[string]string, len(source.Env))
		for _, pair := range source.Env {
			if pair.Key == "" {
				continue
			}
			env[pair.Key] = pair.Value
		}
		if len(env) > 0 {
			template.Env = env
		}
	}

	return template
}

// CreateTemplate creates a new template
func (c *Client) CreateTemplate(req *TemplateCreateRequest) (*Template, error) {
	data, err := c.Post("/templates", req)
	if err != nil {
		return nil, err
	}

	var template Template
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &template, nil
}

// UpdateTemplate updates an existing template
func (c *Client) UpdateTemplate(templateID string, req *TemplateUpdateRequest) (*Template, error) {
	data, err := c.Patch("/templates/"+templateID, req)
	if err != nil {
		return nil, err
	}

	var template Template
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &template, nil
}

// DeleteTemplate deletes a template
func (c *Client) DeleteTemplate(templateID string) error {
	_, err := c.Delete("/templates/" + templateID)
	return err
}
