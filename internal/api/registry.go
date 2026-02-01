package api

import (
	"encoding/json"
	"fmt"
)

// ContainerRegistryAuth represents a container registry authentication
type ContainerRegistryAuth struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

// ContainerRegistryAuthListResponse is the response from listing container registry auths
type ContainerRegistryAuthListResponse struct {
	ContainerRegistryAuths []ContainerRegistryAuth `json:"containerRegistryAuths"`
}

// ContainerRegistryAuthCreateRequest is the request to create a container registry auth
type ContainerRegistryAuthCreateRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListContainerRegistryAuths returns all container registry auths
func (c *Client) ListContainerRegistryAuths() ([]ContainerRegistryAuth, error) {
	data, err := c.Get("/containerregistryauth", nil)
	if err != nil {
		return nil, err
	}

	var auths []ContainerRegistryAuth
	if err := json.Unmarshal(data, &auths); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return auths, nil
}

// GetContainerRegistryAuth returns a single container registry auth by ID
func (c *Client) GetContainerRegistryAuth(authID string) (*ContainerRegistryAuth, error) {
	data, err := c.Get("/containerregistryauth/"+authID, nil)
	if err != nil {
		return nil, err
	}

	var auth ContainerRegistryAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &auth, nil
}

// CreateContainerRegistryAuth creates a new container registry auth
func (c *Client) CreateContainerRegistryAuth(req *ContainerRegistryAuthCreateRequest) (*ContainerRegistryAuth, error) {
	data, err := c.Post("/containerregistryauth", req)
	if err != nil {
		return nil, err
	}

	var auth ContainerRegistryAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &auth, nil
}

// DeleteContainerRegistryAuth deletes a container registry auth
func (c *Client) DeleteContainerRegistryAuth(authID string) error {
	_, err := c.Delete("/containerregistryauth/" + authID)
	return err
}
