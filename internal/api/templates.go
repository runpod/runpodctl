package api

import (
	"encoding/json"
	"fmt"
)

// Template represents a runpod template
type Template struct {
	ID                string            `json:"id"`
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
	Name      string            `json:"name,omitempty"`
	ImageName string            `json:"imageName,omitempty"`
	Ports     []string          `json:"ports,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Readme    string            `json:"readme,omitempty"`
}

// ListTemplates returns all templates
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

// GetTemplate returns a single template by ID
func (c *Client) GetTemplate(templateID string) (*Template, error) {
	data, err := c.Get("/templates/"+templateID, nil)
	if err != nil {
		return nil, err
	}

	var template Template
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &template, nil
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
