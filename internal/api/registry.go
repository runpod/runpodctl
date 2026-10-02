package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// ContainerRegistryAuth represents a container registry authentication
type ContainerRegistryAuth struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

// registryListResponse is the rest v2 registry listing envelope
type registryListResponse struct {
	Registries []ContainerRegistryAuth `json:"registries"`
}

// ContainerRegistryAuthCreateRequest is the request to create a container registry auth
type ContainerRegistryAuthCreateRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListContainerRegistryAuths returns all container registry auths
func (c *Client) ListContainerRegistryAuths() ([]ContainerRegistryAuth, error) {
	data, err := c.GetV2("/registries", nil)
	if err != nil {
		return nil, err
	}

	var resp registryListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return resp.Registries, nil
}

// GetContainerRegistryAuth returns a single container registry auth by ID
func (c *Client) GetContainerRegistryAuth(authID string) (*ContainerRegistryAuth, error) {
	data, err := c.GetV2("/registries/"+url.PathEscape(authID), nil)
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
	data, err := c.PostV2("/registries", req)
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
	_, err := c.DeleteV2("/registries/" + url.PathEscape(authID))
	return err
}
