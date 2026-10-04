package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBillingUsesV2RoutesAndFilters(t *testing.T) {
	tests := []struct {
		name      string
		call      func(c *Client) (interface{}, error)
		wantPath  string
		wantQuery string
		body      string
		wantJSON  string // the records as the cli prints them
	}{
		{
			name: "pods",
			call: func(c *Client) (interface{}, error) {
				return c.GetPodBilling(&BillingOptions{BucketSize: "day", PodID: "pod-1"})
			},
			wantPath:  "/billing/pods",
			wantQuery: "bucketSize=day&podId=pod-1",
			body:      `{"records":[{"startTime":"2026-09-01T00:00:00Z","endTime":"2026-09-02T00:00:00Z","podId":"pod-1","totalAmount":1.5,"gpuAmount":1,"cpuAmount":0.25,"diskAmount":0.25}],"metadata":{}}`,
			wantJSON:  `[{"startTime":"2026-09-01T00:00:00Z","endTime":"2026-09-02T00:00:00Z","podId":"pod-1","totalAmount":1.5,"gpuAmount":1,"cpuAmount":0.25,"diskAmount":0.25}]`,
		},
		{
			// v2's /billing/endpoints bills public endpoints; serverless spend is
			// /billing/serverless, filtered by serverlessId
			name: "serverless is billed on /billing/serverless",
			call: func(c *Client) (interface{}, error) {
				return c.GetEndpointBilling(&BillingOptions{StartTime: "2026-09-01T00:00:00Z", EndpointID: "ep-1"})
			},
			wantPath:  "/billing/serverless",
			wantQuery: "serverlessId=ep-1&startTime=2026-09-01T00%3A00%3A00Z",
			body:      `{"records":[{"startTime":"a","endTime":"b","serverlessId":"ep-1","totalAmount":2,"gpuAmount":2,"cpuAmount":0,"diskAmount":0,"feeAmount":0}],"metadata":{}}`,
			// feeAmount is deprecated in v2 and always 0, so it is not printed
			wantJSON: `[{"startTime":"a","endTime":"b","serverlessId":"ep-1","totalAmount":2,"gpuAmount":2,"cpuAmount":0,"diskAmount":0}]`,
		},
		{
			name: "network volumes",
			call: func(c *Client) (interface{}, error) {
				return c.GetNetworkVolumeBilling(nil)
			},
			wantPath: "/billing/network-volumes",
			body:     `{"records":[{"startTime":"a","endTime":"b","networkVolumeId":"v","totalAmount":1,"standardAmount":1,"highPerformanceAmount":0}],"metadata":{}}`,
			wantJSON: `[{"startTime":"a","endTime":"b","networkVolumeId":"v","totalAmount":1,"standardAmount":1,"highPerformanceAmount":0}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.wantPath || r.URL.RawQuery != tt.wantQuery {
					t.Errorf("got %s?%s, want %s?%s", r.URL.Path, r.URL.RawQuery, tt.wantPath, tt.wantQuery)
				}
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			records, err := tt.call(newV2TestClient(t, server))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := json.Marshal(records)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantJSON {
				t.Fatalf("records = %s\nwant      %s", got, tt.wantJSON)
			}
		})
	}
}

func TestBillingEmptyIsAnEmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"records":null,"metadata":{}}`)
	}))
	defer server.Close()

	records, err := newV2TestClient(t, server).GetPodBilling(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if records == nil || len(records) != 0 {
		t.Fatalf("records = %#v, want an empty non-nil list so json prints []", records)
	}
}
