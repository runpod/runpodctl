package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// NetworkVolume represents a network volume
type NetworkVolume struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DataCenterID string `json:"dataCenterId"`
}

// v2NetworkVolume is a network volume as rest v2 reports it. v2 names the data
// center `dataCenter`; the cli keeps printing it as `dataCenterId`.
type v2NetworkVolume struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int    `json:"size"`
	DataCenter string `json:"dataCenter"`
}

func (v v2NetworkVolume) toNetworkVolume() *NetworkVolume {
	return &NetworkVolume{ID: v.ID, Name: v.Name, Size: v.Size, DataCenterID: v.DataCenter}
}

type v2NetworkVolumeList struct {
	NetworkVolumes []v2NetworkVolume `json:"networkVolumes"`
}

type v2CreateNetworkVolumeRequest struct {
	Name       string `json:"name"`
	Size       int    `json:"size"`
	DataCenter string `json:"dataCenter"`
}

func parseV2NetworkVolume(data []byte) (*NetworkVolume, error) {
	var v v2NetworkVolume
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return v.toNetworkVolume(), nil
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
	data, err := c.GetV2("/network-volumes", nil)
	if err != nil {
		return nil, err
	}

	var list v2NetworkVolumeList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	volumes := make([]NetworkVolume, 0, len(list.NetworkVolumes))
	for _, v := range list.NetworkVolumes {
		volumes = append(volumes, *v.toNetworkVolume())
	}
	return volumes, nil
}

// GetNetworkVolume returns a single network volume by ID
func (c *Client) GetNetworkVolume(volumeID string) (*NetworkVolume, error) {
	data, err := c.GetV2("/network-volumes/"+url.PathEscape(volumeID), nil)
	if err != nil {
		return nil, err
	}
	return parseV2NetworkVolume(data)
}

// CreateNetworkVolume creates a new network volume
func (c *Client) CreateNetworkVolume(req *NetworkVolumeCreateRequest) (*NetworkVolume, error) {
	data, err := c.PostV2("/network-volumes", &v2CreateNetworkVolumeRequest{
		Name:       req.Name,
		Size:       req.Size,
		DataCenter: req.DataCenterID,
	})
	if err != nil {
		return nil, err
	}
	return parseV2NetworkVolume(data)
}

// UpdateNetworkVolume updates an existing network volume
func (c *Client) UpdateNetworkVolume(volumeID string, req *NetworkVolumeUpdateRequest) (*NetworkVolume, error) {
	// v2 rejects an empty patch body, where v1 accepted it as a no-op that
	// returned the volume; read it instead so `update <id>` with no flags still
	// succeeds the same way.
	if req.Name == "" && req.Size == 0 {
		return c.GetNetworkVolume(volumeID)
	}
	data, err := c.PatchV2("/network-volumes/"+url.PathEscape(volumeID), req)
	if err != nil {
		return nil, err
	}
	return parseV2NetworkVolume(data)
}

// DeleteNetworkVolume deletes a network volume
func (c *Client) DeleteNetworkVolume(volumeID string) error {
	_, err := c.DeleteV2("/network-volumes/" + url.PathEscape(volumeID))
	return err
}
