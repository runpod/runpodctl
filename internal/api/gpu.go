package api

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/viper"
)

// GpuType represents a GPU type
type GpuType struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	MemoryInGb     int    `json:"memoryInGb"`
	SecureCloud    bool   `json:"secureCloud"`
	CommunityCloud bool   `json:"communityCloud"`
}

// GpuTypeWithAvailability includes availability info
type GpuTypeWithAvailability struct {
	GpuType
	StockStatus string `json:"stockStatus,omitempty"`
	Available   bool   `json:"available"`
}

// DataCenter represents a data center
type DataCenter struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Location        string                    `json:"location"`
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
	ID                 string  `json:"id"`
	Email              string  `json:"email"`
	ClientBalance      float64 `json:"clientBalance"`
	CurrentSpendPerHr  float64 `json:"currentSpendPerHr"`
	SpendLimit         float64 `json:"spendLimit"`
	NotifyPodsStale    bool    `json:"notifyPodsStale"`
	NotifyPodsGeneral  bool    `json:"notifyPodsGeneral"`
	NotifyLowBalance   bool    `json:"notifyLowBalance"`
}

// graphqlRequest makes a GraphQL request
func (c *Client) graphqlRequest(query string, variables map[string]interface{}) ([]byte, error) {
	apiURL := viper.GetString("apiUrl")
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
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
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

	// build availability map from datacenters
	availabilityMap := make(map[string]string) // gpuTypeId -> best stock status
	for _, dc := range dataCenters {
		for _, avail := range dc.GpuAvailability {
			current, exists := availabilityMap[avail.GpuTypeID]
			// prefer High > Medium > Low
			if !exists || betterStock(avail.StockStatus, current) {
				availabilityMap[avail.GpuTypeID] = avail.StockStatus
			}
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
			GpuType:     gpu,
			StockStatus: stockStatus,
			Available:   available,
		})
	}

	return result, nil
}

func betterStock(a, b string) bool {
	order := map[string]int{"High": 3, "Medium": 2, "Low": 1, "": 0}
	return order[a] > order[b]
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
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
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
		return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
	}

	return resp.Data.Myself, nil
}
