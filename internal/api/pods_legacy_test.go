package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetLegacyPods_FillsMachineFromCatalog(t *testing.T) {
	catalogReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pods":
			_, _ = w.Write([]byte(`{"pods":[
				{"id":"gpu-pod","name":"a","status":"RUNNING","dataCenterId":"EU-RO-1","cost":0.5,
				 "gpu":{"id":"NVIDIA A40","count":2,"vcpuCount":16,"memory":100}},
				{"id":"cpu-pod","name":"b","status":"EXITED","dataCenterId":"US-NC-2",
				 "cpu":{"id":"cpu3g","vcpuCount":2,"memory":8}},
				{"id":"odd-pod","name":"c","status":"RUNNING","dataCenterId":"US-TX-3",
				 "gpu":{"id":"NVIDIA Unlisted","count":1}}
			],"pagination":{"hasNextPage":false}}`))
		case "/catalog/gpus":
			catalogReads++
			_, _ = w.Write([]byte(`{"gpus":[{"id":"NVIDIA A40","name":"A40"}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newV2TestClient(t, server)

	pods, err := client.GetLegacyPods()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string][2]string{
		"gpu-pod": {"A40", "RO"},
		"cpu-pod": {"unknown", "US"},         // graphql reported cpu pods as "unknown"
		"odd-pod": {"NVIDIA Unlisted", "US"}, // not in the catalog: the type id
	}
	for _, p := range pods {
		w := want[p.ID]
		if p.Machine == nil || p.Machine.GpuDisplayName != w[0] || p.Machine.Location != w[1] {
			t.Errorf("%s machine = %+v, want %v", p.ID, p.Machine, w)
		}
		if p.PodType != "RESERVED" {
			t.Errorf("%s podType = %q", p.ID, p.PodType)
		}
	}
	if pods[0].GpuCount != 2 || pods[0].CostPerHr != 0.5 || pods[1].DesiredStatus != "EXITED" {
		t.Errorf("pod fields not mapped: %+v / %+v", pods[0], pods[1])
	}
	if catalogReads != 1 {
		t.Errorf("catalog read %d times, want once", catalogReads)
	}
}

func TestLegacyLocation(t *testing.T) {
	for in, want := range map[string]string{
		"US-NC-2": "US", "CA-MTL-1": "CA", "EU-RO-1": "RO", "EUR-IS-2": "IS",
		"AP-JP-1": "JP", "OC-AU-1": "AU", "SEA-SG-1": "SG", "": "", "LOCAL": "LOCAL",
	} {
		if got := legacyLocation(in); got != want {
			t.Errorf("legacyLocation(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLegacyPodCreate_V2Request(t *testing.T) {
	req := (&LegacyPodCreate{
		CloudType: "COMMUNITY", ImageName: "runpod/base:1.0", GpuTypeID: "NVIDIA A40", GpuCount: 2,
		ContainerDiskInGb: 20, VolumeInGb: 1, VolumeMountPath: "/runpod", DataCenterID: "US-GA-1",
		DockerArgs: "sleep infinity", Ports: []string{"8888/http"}, Env: map[string]string{"A": "1"},
	}).V2Request()

	if req.Name != "runpod/base" {
		t.Errorf("name = %q, want the image without its tag", req.Name)
	}
	if req.GpuTypeID != "NVIDIA A40" || req.GpuCount != 2 || req.CloudType != "COMMUNITY" {
		t.Errorf("gpu/cloud = %q x%d %q", req.GpuTypeID, req.GpuCount, req.CloudType)
	}
	if len(req.DataCenterIDs) != 1 || req.DataCenterIDs[0] != "US-GA-1" {
		t.Errorf("dataCenterIds = %v", req.DataCenterIDs)
	}
	if len(req.Cmd) != 2 || req.Cmd[0] != "sleep" || req.Entrypoint != nil {
		t.Errorf("cmd/entrypoint = %v / %v", req.Cmd, req.Entrypoint)
	}
	// v2's minimum persistent volume is 10 gb; the legacy default of 1 is raised
	if req.VolumeInGb != 10 || req.VolumeMountPath != "/runpod" || req.StartSSH {
		t.Errorf("volume/ssh = %d %q %v", req.VolumeInGb, req.VolumeMountPath, req.StartSSH)
	}

	// a cpu pod cannot have a pod volume, and the legacy default is 1 gb
	cpu := (&LegacyPodCreate{ImageName: "busybox", Name: "named", GpuCount: 0, VolumeInGb: 1}).V2Request()
	if cpu.GpuTypeID != "" || cpu.GpuCount != 0 || cpu.Name != "named" || cpu.VolumeInGb != 0 {
		t.Errorf("cpu create = %+v", cpu)
	}
}

func TestLegacyPodCreate_VolumeNote(t *testing.T) {
	cases := []struct {
		name string
		in   LegacyPodCreate
		want string
	}{
		{"gpu default volume is raised", LegacyPodCreate{GpuTypeID: "g", VolumeInGb: 1}, "note: volume raised from 1 gb to the minimum of 10 gb"},
		{"gpu volume at the minimum", LegacyPodCreate{GpuTypeID: "g", VolumeInGb: 10}, ""},
		{"no volume", LegacyPodCreate{GpuTypeID: "g"}, ""},
		{"network volume replaces the pod volume", LegacyPodCreate{GpuTypeID: "g", VolumeInGb: 1, NetworkVolumeID: "v"}, ""},
		{"cpu has no pod volume to raise", LegacyPodCreate{VolumeInGb: 1}, ""},
	}
	for _, tc := range cases {
		if got := tc.in.VolumeNote(); got != tc.want {
			t.Errorf("%s: note = %q, want %q", tc.name, got, tc.want)
		}
	}
}
