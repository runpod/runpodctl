package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// BillingRecord represents a billing record
type BillingRecord struct {
	Time            string  `json:"time"`
	Amount          float64 `json:"amount"`
	TimeBilledMs    int64   `json:"timeBilledMs,omitempty"`
	DiskSpaceBilled int     `json:"diskSpaceBilledGb,omitempty"`
	PodID           string  `json:"podId,omitempty"`
	EndpointID      string  `json:"endpointId,omitempty"`
	GpuTypeID       string  `json:"gpuTypeId,omitempty"`
}

// BillingOptions are options for billing queries
type BillingOptions struct {
	StartTime  string
	EndTime    string
	BucketSize string // hour, day, week, month, year
	Grouping   string // podId, gpuTypeId, endpointId
	PodID      string
	EndpointID string
	GpuTypeID  string
}

// GetPodBilling returns billing history for pods
func (c *Client) GetPodBilling(opts *BillingOptions) ([]BillingRecord, error) {
	params := url.Values{}
	if opts != nil {
		if opts.StartTime != "" {
			params.Set("startTime", opts.StartTime)
		}
		if opts.EndTime != "" {
			params.Set("endTime", opts.EndTime)
		}
		if opts.BucketSize != "" {
			params.Set("bucketSize", opts.BucketSize)
		}
		if opts.Grouping != "" {
			params.Set("grouping", opts.Grouping)
		}
		if opts.PodID != "" {
			params.Set("podId", opts.PodID)
		}
		if opts.GpuTypeID != "" {
			params.Set("gpuTypeId", opts.GpuTypeID)
		}
	}

	data, err := c.Get("/billing/pods", params)
	if err != nil {
		return nil, err
	}

	var records []BillingRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return records, nil
}

// GetEndpointBilling returns billing history for serverless endpoints
func (c *Client) GetEndpointBilling(opts *BillingOptions) ([]BillingRecord, error) {
	params := url.Values{}
	if opts != nil {
		if opts.StartTime != "" {
			params.Set("startTime", opts.StartTime)
		}
		if opts.EndTime != "" {
			params.Set("endTime", opts.EndTime)
		}
		if opts.BucketSize != "" {
			params.Set("bucketSize", opts.BucketSize)
		}
		if opts.Grouping != "" {
			params.Set("grouping", opts.Grouping)
		}
		if opts.EndpointID != "" {
			params.Set("endpointId", opts.EndpointID)
		}
		if opts.GpuTypeID != "" {
			params.Set("gpuTypeId", opts.GpuTypeID)
		}
	}

	data, err := c.Get("/billing/endpoints", params)
	if err != nil {
		return nil, err
	}

	var records []BillingRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return records, nil
}

// GetNetworkVolumeBilling returns billing history for network volumes
func (c *Client) GetNetworkVolumeBilling(opts *BillingOptions) ([]BillingRecord, error) {
	params := url.Values{}
	if opts != nil {
		if opts.StartTime != "" {
			params.Set("startTime", opts.StartTime)
		}
		if opts.EndTime != "" {
			params.Set("endTime", opts.EndTime)
		}
		if opts.BucketSize != "" {
			params.Set("bucketSize", opts.BucketSize)
		}
	}

	data, err := c.Get("/billing/networkvolumes", params)
	if err != nil {
		return nil, err
	}

	var records []BillingRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return records, nil
}
