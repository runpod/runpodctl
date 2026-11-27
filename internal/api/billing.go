package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	runpod "github.com/runpod/go-sdk/v2"
	"github.com/runpod/go-sdk/v2/lib/billing"
	"github.com/runpod/go-sdk/v2/lib/types"
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

// billingQuery is the shared part of the billing list parameters.
type billingQuery struct {
	start, end *time.Time
	bucket     *types.BillingBucketSize
	// raw holds a time the cli could not parse, sent as typed so the API
	// rejects it with its own validation error, as it always has.
	//
	// BACKWARD_COMPATIBILITY: keeps the API's error for an invalid
	// --start-time or --end-time. Next major: reject it locally.
	raw url.Values
}

func (o *BillingOptions) query() billingQuery {
	q := billingQuery{raw: url.Values{}}
	if o == nil {
		return q
	}
	parse := func(param, value string) *time.Time {
		if value == "" {
			return nil
		}
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			q.raw.Set(param, value)
			return nil
		}
		return &t
	}
	q.start = parse("startTime", o.StartTime)
	q.end = parse("endTime", o.EndTime)
	if o.BucketSize != "" {
		b := types.BillingBucketSize(o.BucketSize)
		q.bucket = &b
	}
	return q
}

// rawTimes adds the times the cli could not parse to the request's query.
func (q billingQuery) rawTimes(_ context.Context, req *http.Request) error {
	if len(q.raw) == 0 {
		return nil
	}
	query := req.URL.Query()
	for k, v := range q.raw {
		query[k] = v
	}
	req.URL.RawQuery = query.Encode()
	return nil
}

// listBillingRecords decodes into the cli's own record types, which match the
// wire shape: the SDK's records parse startTime and endTime into time.Time,
// which would print them differently from what the API sent.
//
// BACKWARD_COMPATIBILITY: keeps the API's timestamp text in the output. Next
// major: decode into the SDK's records.
func listBillingRecords[T any](c *Client, call sdkRequest) ([]T, error) {
	resp, err := sdkCall[struct {
		Records []T `json:"records"`
	}](c, call)
	if err != nil {
		return nil, err
	}
	if resp.Records == nil {
		resp.Records = []T{}
	}
	return resp.Records, nil
}

// GetPodBilling returns billing history for pods
func (c *Client) GetPodBilling(opts *BillingOptions) ([]PodBillingRecord, error) {
	q := opts.query()
	params := &billing.ListPodBillingParams{StartTime: q.start, EndTime: q.end, BucketSize: q.bucket}
	if opts != nil {
		params.PodId = optional(opts.PodID)
	}
	return listBillingRecords[PodBillingRecord](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Billing.ListPodBilling(ctx, params, q.rawTimes)
	})
}

// GetEndpointBilling returns billing history for serverless endpoints. this is
// v2's /billing/serverless: v2's /billing/endpoints bills public endpoints, a
// different product.
func (c *Client) GetEndpointBilling(opts *BillingOptions) ([]ServerlessBillingRecord, error) {
	q := opts.query()
	params := &billing.ListServerlessBillingParams{StartTime: q.start, EndTime: q.end, BucketSize: q.bucket}
	if opts != nil {
		params.ServerlessId = optional(opts.EndpointID)
	}
	return listBillingRecords[ServerlessBillingRecord](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Billing.ListServerlessBilling(ctx, params, q.rawTimes)
	})
}

// GetNetworkVolumeBilling returns billing history for network volumes
func (c *Client) GetNetworkVolumeBilling(opts *BillingOptions) ([]NetworkVolumeBillingRecord, error) {
	q := opts.query()
	params := &billing.ListNetworkVolumeBillingParams{StartTime: q.start, EndTime: q.end, BucketSize: q.bucket}
	return listBillingRecords[NetworkVolumeBillingRecord](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Billing.ListNetworkVolumeBilling(ctx, params, q.rawTimes)
	})
}
