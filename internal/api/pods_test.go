package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pods" {
			t.Errorf("expected /pods, got %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		json.NewEncoder(w).Encode([]Pod{
			{ID: "pod-1", Name: "test-pod-1"},
			{ID: "pod-2", Name: "test-pod-2"},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pods, err := client.ListPods(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 2 {
		t.Errorf("expected 2 pods, got %d", len(pods))
	}
	if pods[0].ID != "pod-1" {
		t.Errorf("expected pod-1, got %s", pods[0].ID)
	}
}

func TestListPods_WithOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("computeType") != "GPU" {
			t.Errorf("expected computeType=GPU")
		}
		json.NewEncoder(w).Encode([]Pod{})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	opts := &PodListOptions{
		ComputeType: "GPU",
	}
	_, err := client.ListPods(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetPod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pods/pod-123" {
			t.Errorf("expected /pods/pod-123, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Pod{
			ID:        "pod-123",
			Name:      "my-pod",
			ImageName: "runpod/pytorch",
			GpuCount:  1,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pod, err := client.GetPod("pod-123", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.ID != "pod-123" {
		t.Errorf("expected pod-123, got %s", pod.ID)
	}
	if pod.Name != "my-pod" {
		t.Errorf("expected my-pod, got %s", pod.Name)
	}
}

func TestGetPod_IncludeNetworkVolumeSurvivesToOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("includeNetworkVolume"); got != "true" {
			t.Errorf("expected includeNetworkVolume=true, got %q", r.URL.RawQuery)
		}
		w.Write([]byte(`{
			"id": "pod-123",
			"name": "my-pod",
			"networkVolumeId": "vol-123",
			"networkVolume": {
				"id": "vol-123",
				"name": "my-volume",
				"dataCenterId": "US-TX-1",
				"size": 10
			}
		}`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pod, err := client.GetPod("pod-123", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	printed, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(printed, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["networkVolumeId"] != "vol-123" {
		t.Errorf("expected networkVolumeId vol-123, got %v", got["networkVolumeId"])
	}

	volume, ok := got["networkVolume"].(map[string]interface{})
	if !ok {
		t.Fatalf("network volume missing from pod output: %s", printed)
	}
	if volume["name"] != "my-volume" {
		t.Errorf("expected my-volume, got %v", volume["name"])
	}
	if volume["dataCenterId"] != "US-TX-1" {
		t.Errorf("expected US-TX-1, got %v", volume["dataCenterId"])
	}
	if volume["size"] != float64(10) {
		t.Errorf("expected size 10, got %v", volume["size"])
	}
}

func TestGetPod_OmitsNetworkVolumeWhenAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id": "pod-123", "name": "my-pod"}`))
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pod, err := client.GetPod("pod-123", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	printed, err := json.Marshal(pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(printed, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, present := got["networkVolumeId"]; present {
		t.Errorf("expected networkVolumeId to be omitted for a pod without a network volume, got %s", printed)
	}
	if _, present := got["networkVolume"]; present {
		t.Errorf("expected networkVolume to be omitted for a pod without a network volume, got %s", printed)
	}
}

func TestCreatePod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/pods" {
			t.Errorf("expected /pods, got %s", r.URL.Path)
		}

		var req PodCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.ImageName != "runpod/pytorch" {
			t.Errorf("expected runpod/pytorch, got %s", req.ImageName)
		}

		json.NewEncoder(w).Encode(Pod{
			ID:        "new-pod-id",
			Name:      req.Name,
			ImageName: req.ImageName,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pod, err := client.CreatePod(&PodCreateRequest{
		Name:      "test-pod",
		ImageName: "runpod/pytorch",
		GpuCount:  1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.ID != "new-pod-id" {
		t.Errorf("expected new-pod-id, got %s", pod.ID)
	}
}

func TestStartPod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/pods/pod-123/start" {
			t.Errorf("expected /pods/pod-123/start, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Pod{ID: "pod-123", DesiredStatus: "RUNNING"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pod, err := client.StartPod("pod-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.DesiredStatus != "RUNNING" {
		t.Errorf("expected RUNNING, got %s", pod.DesiredStatus)
	}
}

func TestStopPod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pods/pod-123/stop" {
			t.Errorf("expected /pods/pod-123/stop, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Pod{ID: "pod-123", DesiredStatus: "EXITED"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	pod, err := client.StopPod("pod-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.DesiredStatus != "EXITED" {
		t.Errorf("expected EXITED, got %s", pod.DesiredStatus)
	}
}

func TestDeletePod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/pods/pod-123" {
			t.Errorf("expected /pods/pod-123, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	err := client.DeletePod("pod-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
