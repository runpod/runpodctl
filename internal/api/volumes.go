package api

import (
	"encoding/json"
	"fmt"
)

// NetworkVolume represents a network volume
type NetworkVolume struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DataCenterID string `json:"dataCenterId"`
}

// NetworkVolumeListResponse is the response from listing network volumes
type NetworkVolumeListResponse struct {
	NetworkVolumes []NetworkVolume `json:"networkVolumes"`
}

// NetworkVolumeCreateRequest is the request to create a network volume
type NetworkVolumeCreateRequest struct {
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DataCenterID string `json:"dataCenterId"`
}

// NetworkVolumeUpdateRequest is the request to update a network volume
type NetworkVolumeUpdateRequest struct {
	Name string `json:"name,omitempty"`
	Size int    `json:"size,omitempty"`
}

// ListNetworkVolumes returns all network volumes
func (c *Client) ListNetworkVolumes() ([]NetworkVolume, error) {
	data, err := c.Get("/networkvolumes", nil)
	if err != nil {
		return nil, err
	}

	var volumes []NetworkVolume
	if err := json.Unmarshal(data, &volumes); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return volumes, nil
}

// GetNetworkVolume returns a single network volume by ID
func (c *Client) GetNetworkVolume(volumeID string) (*NetworkVolume, error) {
	data, err := c.Get("/networkvolumes/"+volumeID, nil)
	if err != nil {
		return nil, err
	}

	var volume NetworkVolume
	if err := json.Unmarshal(data, &volume); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &volume, nil
}

// CreateNetworkVolume creates a new network volume
func (c *Client) CreateNetworkVolume(req *NetworkVolumeCreateRequest) (*NetworkVolume, error) {
	data, err := c.Post("/networkvolumes", req)
	if err != nil {
		return nil, err
	}

	var volume NetworkVolume
	if err := json.Unmarshal(data, &volume); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &volume, nil
}

// UpdateNetworkVolume updates an existing network volume
func (c *Client) UpdateNetworkVolume(volumeID string, req *NetworkVolumeUpdateRequest) (*NetworkVolume, error) {
	data, err := c.Patch("/networkvolumes/"+volumeID, req)
	if err != nil {
		return nil, err
	}

	var volume NetworkVolume
	if err := json.Unmarshal(data, &volume); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &volume, nil
}

// DeleteNetworkVolume deletes a network volume
func (c *Client) DeleteNetworkVolume(volumeID string) error {
	_, err := c.Delete("/networkvolumes/" + volumeID)
	return err
}
