package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/serverless"
	"github.com/runpod/go-sdk/v2/lib/types"
)

// gpuPoolTypes maps each serverless pool to its gpu type ids, from the catalog.
func (c *Client) gpuPoolTypes() (map[string][]string, error) {
	pools, err := c.ListServerlessGpuPools()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(pools))
	for _, p := range pools {
		out[strings.ToUpper(p.ID)] = p.GpuTypeIDs
	}
	return out, nil
}

func (c *Client) getSDKEndpoint(endpointID string) (*types.Endpoint, error) {
	if err := requireID(endpointID); err != nil {
		return nil, err
	}
	return sdkCall[types.Endpoint](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Serverless.GetEndpoint(ctx, endpointID)
	})
}

func (c *Client) listSDKEndpoints() ([]types.Endpoint, error) {
	var endpoints []types.Endpoint
	err := sdkAllPages("/serverless", func(cursor *string) (types.Pagination, error) {
		page, err := sdkCall[types.ListEndpointsResponse](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
			return sdk.Serverless.ListEndpoints(ctx, &serverless.ListEndpointsParams{Cursor: cursor})
		})
		if err != nil {
			return types.Pagination{}, err
		}
		endpoints = append(endpoints, page.Endpoints...)
		return page.Pagination, nil
	})
	return endpoints, err
}

// decorateEndpoint applies the --include-template / --include-workers
// expansions. workers come from the v2 worker listing (the v1 expansion
// reported every worker of a warm endpoint as EXITED). a failed worker read is
// an error: without it the endpoint would print as one with no workers.
func (c *Client) decorateEndpoint(out *Endpoint, e *types.Endpoint, opts *EndpointListOptions, workers *V2Client) error {
	if opts == nil {
		return nil
	}
	if opts.IncludeTemplate {
		out.Template = endpointInlineTemplate(*e)
	}
	if opts.IncludeWorkers {
		list, err := workers.ListEndpointWorkersWithTimeout(e.Id)
		if err != nil {
			return fmt.Errorf("failed to read workers of endpoint %s: %w", e.Id, err)
		}
		out.Workers = make([]interface{}, 0, len(list.Workers))
		for _, w := range list.Workers {
			out.Workers = append(out.Workers, w)
		}
	}
	return nil
}

// workerClient returns the v2 client for the worker listing, sharing this
// client's v2 host and transport. nil when workers are not requested.
func (c *Client) workerClient(opts *EndpointListOptions) (*V2Client, error) {
	if opts == nil || !opts.IncludeWorkers {
		return nil, nil
	}
	return &V2Client{sdk: c.sdk}, nil
}
