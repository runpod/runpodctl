package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListPodsV2PaginatesAndFiltersLocally(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pods" {
			t.Errorf("expected /pods, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "1000" {
			t.Errorf("expected limit=1000, got %q", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected authorization header")
		}
		switch r.URL.Query().Get("cursor") {
		case "":
			json.NewEncoder(w).Encode(podV2ListResponse{
				Pods:       []podV2{{ID: "cpu-1", Name: "ignore", Status: "RUNNING"}},
				Pagination: podV2Pagination{NextCursor: "page-2", HasNextPage: true},
			})
		case "page-2":
			json.NewEncoder(w).Encode(podV2ListResponse{
				Pods: []podV2{{ID: "gpu-1", Name: "demo", Status: "RUNNING", GPU: &podV2GPU{ID: "A100", Count: 1}}},
			})
		default:
			t.Errorf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	pods, err := client.ListPodsV2(&PodListOptions{ComputeType: "GPU", Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].ID != "gpu-1" || pods[0].GpuTypeID != "A100" {
		t.Fatalf("unexpected filtered pods: %#v", pods)
	}
}

func TestGetPodV2MapsFieldsAndIncludeOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/pods/pod-1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(podV2{
			ID: "pod-1", Name: "demo", Image: "image", Status: "RUNNING", Disk: 40,
			GPU:    &podV2GPU{ID: "A100", Count: 2, VCPUCount: 16, Memory: 80},
			Mounts: podV2Mounts{Persistent: &PodV2PersistentMount{Size: 20, Path: "/workspace"}},
			Cloud:  "SECURE", DataCenterID: "US-KS-2",
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	pod, err := client.GetPodV2("pod-1", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if pod.ID != "pod-1" || pod.DesiredStatus != "RUNNING" || pod.ImageName != "image" || pod.GpuTypeID != "A100" {
		t.Fatalf("unexpected pod mapping: %#v", pod)
	}
	if pod.Machine["dataCenterId"] != "US-KS-2" {
		t.Fatalf("machine was not included: %#v", pod.Machine)
	}
}

func TestPodActionsV2UseActionBody(t *testing.T) {
	actions := []string{"stop", "start", "restart"}
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pods/pod-1/action" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		expected := "<none>"
		if requestCount < len(actions) {
			expected = actions[requestCount]
		}
		if requestCount >= len(actions) || body["action"] != expected {
			t.Errorf("expected action %q, got %q", expected, body["action"])
		}
		requestCount++
		json.NewEncoder(w).Encode(podV2{ID: "pod-1", Status: "RUNNING"})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	for _, action := range []func(string) (*Pod, error){client.StopPodV2, client.StartPodV2, client.RestartPodV2} {
		pod, err := action("pod-1")
		if err != nil {
			t.Fatal(err)
		}
		if pod.DesiredStatus != "RUNNING" {
			t.Fatalf("expected RUNNING, got %q", pod.DesiredStatus)
		}
	}
}

func TestCreatePodV2UsesNestedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pods" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		gpu, ok := body["gpu"].(map[string]interface{})
		if !ok || gpu["id"] != "A100" || gpu["count"] != float64(1) {
			t.Errorf("expected nested GPU request, got %#v", body["gpu"])
		}
		json.NewEncoder(w).Encode(podV2{ID: "pod-1", Name: "demo", Image: "image", GPU: &podV2GPU{ID: "A100", Count: 1}})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	pod, err := client.CreatePodV2(&PodV2CreateRequest{Name: "demo", Image: "image", GPU: &PodV2GPURequest{ID: "A100", Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if pod.ID != "pod-1" {
		t.Fatalf("expected pod-1, got %q", pod.ID)
	}
}

func TestCreatePodV2UsesNestedCPURequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		cpu, ok := body["cpu"].(map[string]interface{})
		if !ok || cpu["id"] != "cpu3c" || cpu["vcpuCount"] != float64(2) {
			t.Errorf("expected nested CPU request, got %#v", body["cpu"])
		}
		if _, ok := body["gpu"]; ok {
			t.Errorf("CPU request unexpectedly included GPU config: %#v", body["gpu"])
		}
		json.NewEncoder(w).Encode(podV2{ID: "cpu-pod", CPU: &podV2CPU{ID: "cpu3c", VCPUCount: 2}})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	pod, err := client.CreatePodV2(&PodV2CreateRequest{
		Name: "cpu-demo", Image: "ubuntu:22.04",
		CPU: &PodV2CPURequest{ID: "cpu3c", VCPUCount: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pod.ID != "cpu-pod" {
		t.Fatalf("expected cpu-pod, got %q", pod.ID)
	}
}

func TestUpdatePodV2UsesPatchRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/pods/pod-1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "updated" || body["image"] != "ubuntu:24.04" || body["disk"] != float64(30) {
			t.Errorf("unexpected update request: %#v", body)
		}
		json.NewEncoder(w).Encode(podV2{ID: "pod-1", Name: "updated", Image: "ubuntu:24.04", Disk: 30})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	pod, err := client.UpdatePodV2("pod-1", &PodV2UpdateRequest{Name: "updated", Image: "ubuntu:24.04", Disk: 30})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Name != "updated" || pod.ImageName != "ubuntu:24.04" {
		t.Fatalf("unexpected pod response: %#v", pod)
	}
}

func TestDeletePodV2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/pods/pod-1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	client, err := NewV2Client()
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	if err := client.DeletePodV2("pod-1"); err != nil {
		t.Fatal(err)
	}
}
