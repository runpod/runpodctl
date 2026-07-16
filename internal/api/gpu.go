package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/runpod/runpodctl/internal/configenv"
)

// GpuType represents a GPU type
type GpuType struct {
	ID             string  `json:"id"`
	DisplayName    string  `json:"displayName"`
	MemoryInGb     int     `json:"memoryInGb"`
	SecureCloud    bool    `json:"secureCloud"`
	CommunityCloud bool    `json:"communityCloud"`
	SecurePrice    float64 `json:"securePrice"`
	CommunityPrice float64 `json:"communityPrice"`
}

// GpuDataCenterAvailability is a GPU's stock status in a single data center.
type GpuDataCenterAvailability struct {
	DataCenterID string `json:"dataCenterId"`
	StockStatus  string `json:"stockStatus"`
}

// GpuTypeWithAvailability includes availability info
type GpuTypeWithAvailability struct {
	GpuType
	// StockStatus is the best availability across all data centers.
	StockStatus string `json:"stockStatus,omitempty"`
	Available   bool   `json:"available"`
	// DataCenterAvailability breaks availability down per data center so agents
	// can pick a location that will actually schedule.
	DataCenterAvailability []GpuDataCenterAvailability `json:"dataCenterAvailability,omitempty"`
}

// DataCenter represents a data center
type DataCenter struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	Location        string                        `json:"location"`
	GpuAvailability []GpuAvailabilityInDataCenter `json:"gpuAvailability,omitempty"`
}

// GpuAvailabilityInDataCenter represents GPU availability in a datacenter
type GpuAvailabilityInDataCenter struct {
	GpuTypeID   string `json:"gpuTypeId"`
	DisplayName string `json:"displayName"`
	StockStatus string `json:"stockStatus"`
}

// User represents user account info
type User struct {
	ID                string  `json:"id"`
	Email             string  `json:"email"`
	ClientBalance     float64 `json:"clientBalance"`
	CurrentSpendPerHr float64 `json:"currentSpendPerHr"`
	SpendLimit        float64 `json:"spendLimit"`
	NotifyPodsStale   bool    `json:"notifyPodsStale"`
	NotifyPodsGeneral bool    `json:"notifyPodsGeneral"`
	NotifyLowBalance  bool    `json:"notifyLowBalance"`
}

// graphqlRequest makes a GraphQL request
func (c *Client) graphqlRequest(query string, variables map[string]interface{}) ([]byte, error) {
	apiURL := configenv.GraphQLURL()
	if apiURL == "" {
		apiURL = "https://api.runpod.io/graphql"
	}

	// temporarily swap base URL for GraphQL
	origBaseURL := c.baseURL
	c.baseURL = apiURL
	defer func() { c.baseURL = origBaseURL }()

	body := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	return c.Post("", body)
}

// ListGpuTypes returns all available GPU types (filters out deprecated/unavailable)
func (c *Client) ListGpuTypes(includeUnavailable bool) ([]GpuTypeWithAvailability, error) {
	query := `
		query {
			gpuTypes {
				id
				displayName
				memoryInGb
				secureCloud
				communityCloud
				securePrice
				communityPrice
			}
		}
	`

	data, err := c.graphqlRequest(query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			GpuTypes []GpuType `json:"gpuTypes"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	// get availability from datacenters
	dataCenters, err := c.ListDataCenters()
	if err != nil {
		// if we can't get availability, just return GPU types without it
		var result []GpuTypeWithAvailability
		for _, gpu := range resp.Data.GpuTypes {
			if includeUnavailable || (gpu.SecureCloud || gpu.CommunityCloud) {
				result = append(result, GpuTypeWithAvailability{
					GpuType:   gpu,
					Available: gpu.SecureCloud || gpu.CommunityCloud,
				})
			}
		}
		return result, nil
	}

	// build availability maps from datacenters: the best stock status overall
	// and the per-data-center breakdown.
	availabilityMap := make(map[string]string)               // gpuTypeId -> best stock status
	perDCMap := make(map[string][]GpuDataCenterAvailability) // gpuTypeId -> per-dc availability
	for _, dc := range dataCenters {
		for _, avail := range dc.GpuAvailability {
			current, exists := availabilityMap[avail.GpuTypeID]
			// prefer High > Medium > Low
			if !exists || betterStock(avail.StockStatus, current) {
				availabilityMap[avail.GpuTypeID] = avail.StockStatus
			}
			// list every data center the gpu appears in, but make an unreported
			// status explicit ("none") rather than an empty string, so an agent
			// can tell "offered here, currently no stock" from a missing entry.
			stock := avail.StockStatus
			if stock == "" {
				stock = "none"
			}
			perDCMap[avail.GpuTypeID] = append(perDCMap[avail.GpuTypeID], GpuDataCenterAvailability{
				DataCenterID: dc.ID,
				StockStatus:  stock,
			})
		}
	}

	var result []GpuTypeWithAvailability
	for _, gpu := range resp.Data.GpuTypes {
		stockStatus, hasStock := availabilityMap[gpu.ID]
		available := hasStock && stockStatus != ""

		// filter out GPUs with no availability unless includeUnavailable
		if !includeUnavailable && !available {
			continue
		}

		// filter out "unknown" GPU type
		if gpu.ID == "unknown" {
			continue
		}

		result = append(result, GpuTypeWithAvailability{
			GpuType:                gpu,
			StockStatus:            stockStatus,
			Available:              available,
			DataCenterAvailability: perDCMap[gpu.ID],
		})
	}

	return result, nil
}

func betterStock(a, b string) bool {
	order := map[string]int{"High": 3, "Medium": 2, "Low": 1, "": 0}
	return order[a] > order[b]
}

// ServerlessGpuPool is a serverless gpu pool. saveEndpoint's gpuIds field
// accepts pool ids (e.g. "ADA_24"), not the gpu type ids (e.g. "NVIDIA A40")
// that 'gpu list' and --gpu-id use, so we map between them with this.
type ServerlessGpuPool struct {
	ID         string   `json:"id"`
	GpuTypeIDs []string `json:"gpuTypeIds"`
}

// ListServerlessGpuPools returns the serverless gpu pools and their member
// gpu type ids.
func (c *Client) ListServerlessGpuPools() ([]ServerlessGpuPool, error) {
	query := `
		query {
			serverlessGpuPools {
				id
				gpuTypeIds
			}
		}
	`

	data, err := c.graphqlRequest(query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			ServerlessGpuPools []ServerlessGpuPool `json:"serverlessGpuPools"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	return resp.Data.ServerlessGpuPools, nil
}

// ResolveServerlessGpuPoolID maps a --gpu-id value to the gpu pool id(s) that
// saveEndpoint expects. it accepts a pool id (returned as-is), a gpu type id
// (translated to its pool id), or a comma-separated mix of those (e.g. a hub
// config's gpu list). if the pools query is unavailable it falls back to the
// input unchanged so an already-correct pool id still works (the server
// validates either way).
func (c *Client) ResolveServerlessGpuPoolID(gpuID string) (string, error) {
	pools, err := c.ListServerlessGpuPools()
	if err != nil {
		return gpuID, nil
	}

	poolIDs := make([]string, 0, len(pools))
	for _, p := range pools {
		poolIDs = append(poolIDs, p.ID)
	}

	resolveOne := func(id string) (string, bool) {
		for _, p := range pools {
			if strings.EqualFold(p.ID, id) {
				return p.ID, true
			}
		}
		for _, p := range pools {
			for _, t := range p.GpuTypeIDs {
				if strings.EqualFold(t, id) {
					return p.ID, true
				}
			}
		}
		return "", false
	}

	seen := make(map[string]bool)
	resolved := make([]string, 0)
	for _, tok := range strings.Split(gpuID, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		poolID, ok := resolveOne(tok)
		if !ok {
			return "", fmt.Errorf("unknown gpu id %q; use a gpu id from 'runpodctl gpu list' or a pool id (one of: %s)", tok, strings.Join(poolIDs, ", "))
		}
		if !seen[poolID] {
			seen[poolID] = true
			resolved = append(resolved, poolID)
		}
	}

	return strings.Join(resolved, ","), nil
}

// ListDataCenters returns all data centers with GPU availability
func (c *Client) ListDataCenters() ([]DataCenter, error) {
	query := `
		query {
			dataCenters {
				id
				name
				location
				gpuAvailability {
					gpuTypeId
					displayName
					stockStatus
				}
			}
		}
	`

	data, err := c.graphqlRequest(query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			DataCenters []DataCenter `json:"dataCenters"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	return resp.Data.DataCenters, nil
}

// GetUser returns the current user's account info
func (c *Client) GetUser() (*User, error) {
	query := `
		query {
			myself {
				id
				email
				clientBalance
				currentSpendPerHr
				spendLimit
				notifyPodsStale
				notifyPodsGeneral
				notifyLowBalance
			}
		}
	`

	data, err := c.graphqlRequest(query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Myself *User `json:"myself"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return nil, newGraphQLError(resp.Errors[0].Message)
	}

	return resp.Data.Myself, nil
}
