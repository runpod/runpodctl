package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newV2TestClient points a client's rest v2 traffic at server.
func newV2TestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = "http://v1.invalid"
	client.v2BaseURL = server.URL
	return client
}

func TestListNetworkVolumes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/network-volumes" {
			t.Errorf("expected /network-volumes, got %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"networkVolumes":[
			{"id":"vol-1","name":"volume-1","size":100,"dataCenter":"EU-RO-1","type":"STANDARD"},
			{"id":"vol-2","name":"volume-2","size":200,"dataCenter":"US-TX-1","type":"STANDARD"}]}`)
	}))
	defer server.Close()

	volumes, err := newV2TestClient(t, server).ListNetworkVolumes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []NetworkVolume{
		{ID: "vol-1", Name: "volume-1", Size: 100, DataCenterID: "EU-RO-1"},
		{ID: "vol-2", Name: "volume-2", Size: 200, DataCenterID: "US-TX-1"},
	}
	if len(volumes) != len(want) || volumes[0] != want[0] || volumes[1] != want[1] {
		t.Fatalf("volumes = %+v, want %+v", volumes, want)
	}
}

func TestGetNetworkVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/network-volumes/vol-123" {
			t.Errorf("expected /network-volumes/vol-123, got %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"vol-123","name":"my-volume","size":500,"dataCenter":"US-TX-1","type":"STANDARD"}`)
	}))
	defer server.Close()

	volume, err := newV2TestClient(t, server).GetNetworkVolume("vol-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := NetworkVolume{ID: "vol-123", Name: "my-volume", Size: 500, DataCenterID: "US-TX-1"}
	if *volume != want {
		t.Fatalf("volume = %+v, want %+v", *volume, want)
	}
}

func TestCreateNetworkVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/network-volumes" {
			t.Errorf("expected POST /network-volumes, got %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["dataCenter"] != "US-TX-1" || body["dataCenterId"] != nil {
			t.Errorf("body = %v, want dataCenter and no dataCenterId", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"new-vol-id","name":"test-volume","size":100,"dataCenter":"US-TX-1","type":"STANDARD"}`)
	}))
	defer server.Close()

	volume, err := newV2TestClient(t, server).CreateNetworkVolume(&NetworkVolumeCreateRequest{
		Name:         "test-volume",
		Size:         100,
		DataCenterID: "US-TX-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if volume.ID != "new-vol-id" || volume.DataCenterID != "US-TX-1" {
		t.Errorf("volume = %+v", *volume)
	}
}

func TestUpdateNetworkVolume(t *testing.T) {
	tests := []struct {
		name       string
		req        NetworkVolumeUpdateRequest
		wantMethod string
	}{
		{name: "fields present are patched", req: NetworkVolumeUpdateRequest{Size: 20}, wantMethod: http.MethodPatch},
		{name: "no fields reads the volume instead of sending an empty patch", req: NetworkVolumeUpdateRequest{}, wantMethod: http.MethodGet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				if r.URL.Path != "/network-volumes/vol-123" {
					t.Errorf("expected /network-volumes/vol-123, got %s", r.URL.Path)
				}
				_, _ = io.WriteString(w, `{"id":"vol-123","name":"v","size":20,"dataCenter":"EU-RO-1","type":"STANDARD"}`)
			}))
			defer server.Close()

			volume, err := newV2TestClient(t, server).UpdateNetworkVolume("vol-123", &tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != tt.wantMethod {
				t.Fatalf("method = %s, want %s", gotMethod, tt.wantMethod)
			}
			if volume.DataCenterID != "EU-RO-1" {
				t.Fatalf("volume = %+v", *volume)
			}
		})
	}
}

func TestDeleteNetworkVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/network-volumes/vol-123" {
			t.Errorf("expected DELETE /network-volumes/vol-123, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := newV2TestClient(t, server).DeleteNetworkVolume("vol-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
