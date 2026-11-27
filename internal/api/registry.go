package api

import (
	"context"
	"net/http"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/types"
)

// ContainerRegistryAuth is the registry auth the cli prints. It is kept
// instead of the SDK's type so v3 output stays backward compatible. Remove in
// the next major release.
type ContainerRegistryAuth struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ContainerRegistryAuthCreateRequest is the request to create a container registry auth
type ContainerRegistryAuthCreateRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ListContainerRegistryAuths returns all container registry auths. v2 returns
// the whole list in one page today; walking pages keeps a large account whole
// if that changes.
func (c *Client) ListContainerRegistryAuths() ([]ContainerRegistryAuth, error) {
	auths := []ContainerRegistryAuth{}
	err := sdkAllPages("/registries", func(cursor *string) (types.Pagination, error) {
		page, err := sdkCall[struct {
			types.ListRegistriesResponse
			Pagination types.Pagination `json:"pagination"`
		}](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
			return sdk.Registries.ListRegistries(ctx, withCursor(cursor))
		})
		if err != nil {
			return types.Pagination{}, err
		}
		for _, r := range page.Registries {
			auths = append(auths, registryAuthFromSDK(r))
		}
		return page.Pagination, nil
	})
	if err != nil {
		return nil, err
	}
	return auths, nil
}

// GetContainerRegistryAuth returns a single container registry auth by ID
func (c *Client) GetContainerRegistryAuth(authID string) (*ContainerRegistryAuth, error) {
	if err := requireID(authID); err != nil {
		return nil, err
	}
	r, err := sdkCall[types.Registry](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Registries.GetRegistry(ctx, authID)
	})
	if err != nil {
		return nil, err
	}
	out := registryAuthFromSDK(*r)
	return &out, nil
}

// CreateContainerRegistryAuth creates a new container registry auth
func (c *Client) CreateContainerRegistryAuth(req *ContainerRegistryAuthCreateRequest) (*ContainerRegistryAuth, error) {
	body := types.CreateRegistryRequest{Name: req.Name, Username: req.Username, Password: req.Password}
	r, err := sdkCall[types.Registry](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Registries.CreateRegistry(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	out := registryAuthFromSDK(*r)
	return &out, nil
}

// DeleteContainerRegistryAuth deletes a container registry auth
func (c *Client) DeleteContainerRegistryAuth(authID string) error {
	if err := requireID(authID); err != nil {
		return err
	}
	_, err := c.sdkSend(func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Registries.DeleteRegistry(ctx, authID)
	})
	return err
}
