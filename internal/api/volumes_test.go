package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListNetworkVolumes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/networkvolumes" {
			t.Errorf("expected /networkvolumes, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]NetworkVolume{
			{ID: "vol-1", Name: "volume-1", Size: 100},
			{ID: "vol-2", Name: "volume-2", Size: 200},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	volumes, err := client.ListNetworkVolumes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(volumes) != 2 {
		t.Errorf("expected 2 volumes, got %d", len(volumes))
	}
}

func TestGetNetworkVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(NetworkVolume{
			ID:           "vol-123",
			Name:         "my-volume",
			Size:         500,
			DataCenterID: "US-TX-1",
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	volume, err := client.GetNetworkVolume("vol-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if volume.ID != "vol-123" {
		t.Errorf("expected vol-123, got %s", volume.ID)
	}
	if volume.Size != 500 {
		t.Errorf("expected 500, got %d", volume.Size)
	}
}

func TestCreateNetworkVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req NetworkVolumeCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(NetworkVolume{
			ID:           "new-vol-id",
			Name:         req.Name,
			Size:         req.Size,
			DataCenterID: req.DataCenterID,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	volume, err := client.CreateNetworkVolume(&NetworkVolumeCreateRequest{
		Name:         "test-volume",
		Size:         100,
		DataCenterID: "US-TX-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if volume.ID != "new-vol-id" {
		t.Errorf("expected new-vol-id, got %s", volume.ID)
	}
}

func TestDeleteNetworkVolume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	err := client.DeleteNetworkVolume("vol-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
