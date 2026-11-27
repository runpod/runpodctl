package api

import (
	"context"
	"net/http"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/networkvolumes"
	"github.com/runpod/go-sdk/v2/lib/types"
)

// NetworkVolume is the network volume the cli prints. It is kept instead of
// the SDK's type so v3 output stays backward compatible (dataCenterId, no
// costCenter or tags). Remove in the next major release.
type NetworkVolume struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DataCenterID string `json:"dataCenterId"`
}

// NetworkVolumeCreateRequest is the request to create a network volume. It is
// not the wire shape: CreateNetworkVolume maps it to v2's body, which names the
// data center "dataCenter".
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
	volumes := []NetworkVolume{}
	err := sdkAllPages("/network-volumes", func(cursor *string) (types.Pagination, error) {
		page, err := sdkCall[types.ListNetworkVolumesResponse](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
			return sdk.NetworkVolumes.ListNetworkVolumes(ctx, &networkvolumes.ListNetworkVolumesParams{Cursor: cursor})
		})
		if err != nil {
			return types.Pagination{}, err
		}
		for _, v := range page.NetworkVolumes {
			volumes = append(volumes, networkVolumeFromSDK(v))
		}
		return page.Pagination, nil
	})
	if err != nil {
		return nil, err
	}
	return volumes, nil
}

// GetNetworkVolume returns a single network volume by ID
func (c *Client) GetNetworkVolume(volumeID string) (*NetworkVolume, error) {
	if err := requireID(volumeID); err != nil {
		return nil, err
	}
	v, err := sdkCall[types.NetworkVolume](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.NetworkVolumes.GetNetworkVolume(ctx, volumeID)
	})
	if err != nil {
		return nil, err
	}
	out := networkVolumeFromSDK(*v)
	return &out, nil
}

// CreateNetworkVolume creates a new network volume
func (c *Client) CreateNetworkVolume(req *NetworkVolumeCreateRequest) (*NetworkVolume, error) {
	body := types.CreateNetworkVolumeRequest{Name: req.Name, Size: req.Size, DataCenter: req.DataCenterID}
	v, err := sdkCall[types.NetworkVolume](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.NetworkVolumes.CreateNetworkVolume(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	out := networkVolumeFromSDK(*v)
	return &out, nil
}

// UpdateNetworkVolume updates an existing network volume
func (c *Client) UpdateNetworkVolume(volumeID string, req *NetworkVolumeUpdateRequest) (*NetworkVolume, error) {
	// v2 rejects an empty patch body, where v1 accepted it as a no-op that
	// returned the volume; read it instead so `update <id>` with no flags still
	// succeeds the same way.
	if req.Name == "" && req.Size == 0 {
		return c.GetNetworkVolume(volumeID)
	}
	if err := requireID(volumeID); err != nil {
		return nil, err
	}
	var body types.UpdateNetworkVolumeRequest
	if req.Name != "" {
		body.Name = &req.Name
	}
	if req.Size != 0 {
		body.Size = &req.Size
	}
	v, err := sdkCall[types.NetworkVolume](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.NetworkVolumes.UpdateNetworkVolume(ctx, volumeID, body)
	})
	if err != nil {
		return nil, err
	}
	out := networkVolumeFromSDK(*v)
	return &out, nil
}

// DeleteNetworkVolume deletes a network volume
func (c *Client) DeleteNetworkVolume(volumeID string) error {
	if err := requireID(volumeID); err != nil {
		return err
	}
	_, err := c.sdkSend(func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.NetworkVolumes.DeleteNetworkVolume(ctx, volumeID)
	})
	return err
}
