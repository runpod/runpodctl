package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// Billing records are printed in the rest v2 shape: one record per resource per
// time bucket, with the cost split into its components. v2 has no grouping by
// gpu type and no gpu filter, which v1 had.

// PodBillingRecord is one pod's cost for one time bucket.
type PodBillingRecord struct {
	StartTime   string  `json:"startTime"`
	EndTime     string  `json:"endTime"`
	PodID       string  `json:"podId"`
	TotalAmount float64 `json:"totalAmount"`
	GpuAmount   float64 `json:"gpuAmount"`
	CpuAmount   float64 `json:"cpuAmount"`
	DiskAmount  float64 `json:"diskAmount"`
}

// ServerlessBillingRecord is one serverless endpoint's cost for one time bucket.
type ServerlessBillingRecord struct {
	StartTime    string  `json:"startTime"`
	EndTime      string  `json:"endTime"`
	ServerlessID string  `json:"serverlessId"`
	TotalAmount  float64 `json:"totalAmount"`
	GpuAmount    float64 `json:"gpuAmount"`
	CpuAmount    float64 `json:"cpuAmount"`
	DiskAmount   float64 `json:"diskAmount"`
	FeeAmount    float64 `json:"feeAmount"`
}

// NetworkVolumeBillingRecord is one network volume's cost for one time bucket.
type NetworkVolumeBillingRecord struct {
	StartTime             string  `json:"startTime"`
	EndTime               string  `json:"endTime"`
	NetworkVolumeID       string  `json:"networkVolumeId"`
	TotalAmount           float64 `json:"totalAmount"`
	StandardAmount        float64 `json:"standardAmount"`
	HighPerformanceAmount float64 `json:"highPerformanceAmount"`
}

// BillingOptions are options for billing queries
type BillingOptions struct {
	StartTime  string
	EndTime    string
	BucketSize string // hour, day, week, month, year
	PodID      string
	EndpointID string
}

func (o *BillingOptions) params(idParam, id string) url.Values {
	params := url.Values{}
	if o == nil {
		return params
	}
	if o.StartTime != "" {
		params.Set("startTime", o.StartTime)
	}
	if o.EndTime != "" {
		params.Set("endTime", o.EndTime)
	}
	if o.BucketSize != "" {
		params.Set("bucketSize", o.BucketSize)
	}
	if id != "" {
		params.Set(idParam, id)
	}
	return params
}

func getBillingRecords[T any](c *Client, path string, params url.Values) ([]T, error) {
	data, err := c.GetV2(path, params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Records []T `json:"records"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if resp.Records == nil {
		resp.Records = []T{}
	}
	return resp.Records, nil
}

// GetPodBilling returns billing history for pods
func (c *Client) GetPodBilling(opts *BillingOptions) ([]PodBillingRecord, error) {
	var podID string
	if opts != nil {
		podID = opts.PodID
	}
	return getBillingRecords[PodBillingRecord](c, "/billing/pods", opts.params("podId", podID))
}

// GetEndpointBilling returns billing history for serverless endpoints. this is
// v2's /billing/serverless: v2's /billing/endpoints bills public endpoints, a
// different product.
func (c *Client) GetEndpointBilling(opts *BillingOptions) ([]ServerlessBillingRecord, error) {
	var endpointID string
	if opts != nil {
		endpointID = opts.EndpointID
	}
	return getBillingRecords[ServerlessBillingRecord](c, "/billing/serverless", opts.params("serverlessId", endpointID))
}

// GetNetworkVolumeBilling returns billing history for network volumes
func (c *Client) GetNetworkVolumeBilling(opts *BillingOptions) ([]NetworkVolumeBillingRecord, error) {
	return getBillingRecords[NetworkVolumeBillingRecord](c, "/billing/network-volumes", opts.params("", ""))
}
