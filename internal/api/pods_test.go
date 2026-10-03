package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func v2PodServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
}

const v2PodListBody = `{"pods":[
	{"id":"pod-1","name":"Test-Pod-1","status":"RUNNING","gpu":{"id":"NVIDIA A40","count":1,"vcpuCount":9,"memory":50},"dataCenterId":"EU-RO-1"},
	{"id":"pod-2","name":"test-pod-2","status":"EXITED","cpu":{"id":"cpu3g","vcpuCount":2,"memory":8},"dataCenterId":"US-NC-2"}],
	"pagination":{"hasNextPage":false,"nextCursor":null}}`

func TestListPods(t *testing.T) {
	server := v2PodServer(t, map[string]string{"/pods": v2PodListBody})
	defer server.Close()

	pods, err := newV2TestClient(t, server).ListPods(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods) != 2 || pods[0].ID != "pod-1" || pods[1].ID != "pod-2" {
		t.Fatalf("pods = %+v", pods)
	}
	if pods[0].DesiredStatus != "RUNNING" || pods[0].GpuTypeID != "NVIDIA A40" || pods[0].GpuCount != 1 || pods[0].MemoryInGb != 50 || pods[0].VcpuCount != 9 {
		t.Errorf("gpu pod = %+v", pods[0])
	}
	if pods[1].DesiredStatus != "EXITED" || pods[1].GpuCount != 0 || pods[1].MemoryInGb != 8 || pods[1].VcpuCount != 2 {
		t.Errorf("cpu pod = %+v", pods[1])
	}
}

func TestListPods_WithOptions(t *testing.T) {
	tests := []struct {
		name string
		opts PodListOptions
		want []string
	}{
		// v1's name filter: exact and case-insensitive
		{name: "name matches exactly, ignoring case", opts: PodListOptions{Name: "test-pod-1"}, want: []string{"pod-1"}},
		{name: "a partial name matches nothing", opts: PodListOptions{Name: "test-pod"}, want: nil},
		{name: "gpu type", opts: PodListOptions{GpuTypeIDs: []string{"nvidia a40"}}, want: []string{"pod-1"}},
		{name: "data center", opts: PodListOptions{DataCenterIDs: []string{"US-NC-2"}}, want: []string{"pod-2"}},
		{name: "compute type gpu", opts: PodListOptions{ComputeType: "GPU"}, want: []string{"pod-1"}},
		{name: "compute type cpu", opts: PodListOptions{ComputeType: "cpu"}, want: []string{"pod-2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := v2PodServer(t, map[string]string{"/pods": v2PodListBody})
			defer server.Close()

			pods, err := newV2TestClient(t, server).ListPods(&tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var got []string
			for _, p := range pods {
				got = append(got, p.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("pods = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPod(t *testing.T) {
	server := v2PodServer(t, map[string]string{
		"/pods/pod-123": `{"id":"pod-123","name":"my-pod","status":"STARTING","createdAt":"2026-10-02T05:28:38.366Z",
			"image":"img:1","gpu":{"id":"NVIDIA A40","count":2,"vcpuCount":16,"memory":100},"disk":20,
			"mounts":{"persistent":{"size":30,"path":"/workspace"}},"ports":["22/tcp"],"cmd":["sleep"],"entrypoint":["bash"],
			"cost":0.8,"env":{"A":"1"},"dataCenterId":"EU-RO-1","cloud":"SECURE","runtime":null}`,
	})
	defer server.Close()

	pod, err := newV2TestClient(t, server).GetPod("pod-123", true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Pod{
		ID: "pod-123", Name: "my-pod", DesiredStatus: "RUNNING", CreatedAt: "2026-10-02 05:28:38.366 +0000 UTC",
		ImageName: "img:1", GpuTypeID: "NVIDIA A40", GpuCount: 2, VolumeInGb: 30, ContainerDiskInGb: 20,
		MemoryInGb: 100, VcpuCount: 16, VolumeMountPath: "/workspace", Ports: []string{"22/tcp"},
		DockerStartCmd: []string{"sleep"}, DockerEntrypoint: []string{"bash"}, CostPerHr: 0.8,
		Env:     map[string]string{"A": "1"},
		Machine: map[string]interface{}{"dataCenterId": "EU-RO-1", "secureCloud": true},
	}
	got := *pod
	got.legacy = nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pod =\n%+v\nwant\n%+v", got, want)
	}
	if pod.Legacy() == nil || pod.Legacy().Runtime != nil || pod.Legacy().DesiredStatus != "RUNNING" {
		t.Errorf("runtime view = %+v, want no runtime yet", pod.Legacy())
	}
}

func TestGetPod_IncludeNetworkVolumeSurvivesToOutput(t *testing.T) {
	server := v2PodServer(t, map[string]string{
		"/pods/pod-123":            `{"id":"pod-123","status":"RUNNING","mounts":{"network":[{"volumeId":"vol-123","path":"/data"}]}}`,
		"/network-volumes/vol-123": `{"id":"vol-123","name":"my-volume","dataCenter":"US-TX-1","size":10}`,
	})
	defer server.Close()

	pod, err := newV2TestClient(t, server).GetPod("pod-123", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pod.NetworkVolumeID != "vol-123" || pod.VolumeMountPath != "/data" {
		t.Errorf("pod = %+v", *pod)
	}
	want := NetworkVolume{ID: "vol-123", Name: "my-volume", Size: 10, DataCenterID: "US-TX-1"}
	if pod.NetworkVolume == nil || *pod.NetworkVolume != want {
		t.Fatalf("networkVolume = %+v, want %+v", pod.NetworkVolume, want)
	}
	out, _ := json.Marshal(pod)
	if !strings.Contains(string(out), `"networkVolume":{`) {
		t.Errorf("networkVolume missing from output: %s", out)
	}
}

func TestGetPod_OmitsNetworkVolumeWhenAbsent(t *testing.T) {
	server := v2PodServer(t, map[string]string{"/pods/pod-123": `{"id":"pod-123","status":"RUNNING","mounts":{}}`})
	defer server.Close()

	pod, err := newV2TestClient(t, server).GetPod("pod-123", true, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, _ := json.Marshal(pod)
	if strings.Contains(string(out), "networkVolume") {
		t.Errorf("unexpected networkVolume in output: %s", out)
	}
}

func TestIsPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"198.13.252.95": true,
		"100.65.17.95":  false, // runpod proxy, carrier-grade nat
		"10.0.0.1":      false,
		"192.168.1.1":   false,
		"127.0.0.1":     false,
		"":              false,
	} {
		if got := isPublicIP(ip); got != want {
			t.Errorf("isPublicIP(%q) = %v, want %v", ip, got, want)
		}
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

func TestPodActions(t *testing.T) {
	tests := []struct {
		name   string
		call   func(c *Client) (*Pod, error)
		action string
		body   string // the action response; empty means 204
	}{
		{name: "start", call: func(c *Client) (*Pod, error) { return c.StartPod("pod-1") }, action: "start", body: `{"id":"pod-1","status":"STARTING"}`},
		{name: "stop", call: func(c *Client) (*Pod, error) { return c.StopPod("pod-1") }, action: "stop", body: `{"id":"pod-1","status":"EXITED"}`},
		{name: "restart answered with 204 reads the pod", call: func(c *Client) (*Pod, error) { return c.RestartPod("pod-1") }, action: "restart", body: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/pods/pod-1/action":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["action"] != tt.action {
						t.Errorf("action = %q, want %q", body["action"], tt.action)
					}
					if tt.body == "" {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					_, _ = io.WriteString(w, tt.body)
				case r.Method == http.MethodGet && r.URL.Path == "/pods/pod-1":
					_, _ = io.WriteString(w, `{"id":"pod-1","status":"RUNNING"}`)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()

			pod, err := tt.call(newV2TestClient(t, server))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pod.ID != "pod-1" || pod.DesiredStatus == "" {
				t.Fatalf("pod = %+v", *pod)
			}
		})
	}
}

func TestUpdatePodCompletesVolumeFromCurrentMount(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		req        PodUpdateRequest
		wantMounts string
	}{
		{name: "size only keeps the path", current: `{"id":"p","status":"RUNNING","mounts":{"persistent":{"size":20,"path":"/workspace"}}}`,
			req: PodUpdateRequest{VolumeInGb: 40}, wantMounts: `{"persistent":{"size":40,"path":"/workspace"}}`},
		{name: "path only keeps the size", current: `{"id":"p","status":"RUNNING","mounts":{"persistent":{"size":20,"path":"/workspace"}}}`,
			req: PodUpdateRequest{VolumeMountPath: "/data"}, wantMounts: `{"persistent":{"size":20,"path":"/data"}}`},
		{name: "a network volume only moves its path", current: `{"id":"p","status":"RUNNING","mounts":{"network":[{"volumeId":"v1","path":"/workspace"}]}}`,
			req: PodUpdateRequest{VolumeMountPath: "/data"}, wantMounts: `{"network":[{"volumeId":"v1","path":"/data"}]}`},
		{name: "no volume change sends no mounts", current: "", req: PodUpdateRequest{Name: "n"}, wantMounts: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					_, _ = io.WriteString(w, tt.current)
				case http.MethodPatch:
					var body map[string]json.RawMessage
					_ = json.NewDecoder(r.Body).Decode(&body)
					if got := string(body["mounts"]); got != tt.wantMounts {
						t.Errorf("mounts = %s, want %s", got, tt.wantMounts)
					}
					_, _ = io.WriteString(w, `{"id":"p","status":"RUNNING"}`)
				}
			}))
			defer server.Close()

			if _, err := newV2TestClient(t, server).UpdatePod("p", &tt.req); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeletePod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/pods/pod-123" {
			t.Errorf("expected DELETE /pods/pod-123, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := newV2TestClient(t, server).DeletePod("pod-123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// a network volume's size belongs to the volume, so a pod update must refuse
// to "resize" it rather than send a patch that changes nothing
func TestUpdatePodRefusesToResizeANetworkVolume(t *testing.T) {
	patched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		_, _ = w.Write([]byte(`{"id":"p","status":"RUNNING","mounts":{"network":[{"volumeId":"vol-1","path":"/workspace"}]}}`))
	}))
	defer server.Close()

	_, err := newV2TestClient(t, server).UpdatePod("p", &PodUpdateRequest{VolumeInGb: 100})
	if err == nil || !strings.Contains(err.Error(), "volume update vol-1") {
		t.Fatalf("expected a pointer to volume update, got %v", err)
	}
	if patched {
		t.Error("nothing may be patched")
	}
}
