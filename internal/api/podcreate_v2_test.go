package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// createServer fakes the catalog and POST /pods. postStatus answers each
// create in turn (201 when exhausted).
func createServer(t *testing.T, postStatus []int, posts *[]map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/catalog/cpus":
			_, _ = io.WriteString(w, `{"cpus":[{"id":"cpu3c","availability":"NONE"},{"id":"cpu5g","availability":"MEDIUM"},{"id":"cpu3g","availability":"LOW"}]}`)
		case r.URL.Path == "/catalog/gpus":
			if r.URL.Query().Get("countryCodes") != "DE" {
				t.Errorf("countryCodes = %q", r.URL.Query().Get("countryCodes"))
			}
			_, _ = io.WriteString(w, `{"gpus":[{"id":"NVIDIA A40","dataCenters":[{"id":"EU-DE-1"},{"id":"EU-DE-2"}]},{"id":"NVIDIA L4","dataCenters":[{"id":"EU-DE-9"}]}]}`)
		case r.URL.Path == "/catalog/datacenters":
			_, _ = io.WriteString(w, `{"dataCenters":[{"id":"EU-DE-2"},{"id":"US-TX-3"}]}`)
		case r.URL.Path == "/pods" && r.Method == http.MethodPost:
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*posts = append(*posts, body)
			if n := len(*posts); n <= len(postStatus) && postStatus[n-1] != http.StatusCreated {
				w.WriteHeader(postStatus[n-1])
				_, _ = fmt.Fprintf(w, `{"title":"x","status":%d,"detail":"There are no longer any instances available"}`, postStatus[n-1])
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"new-pod","name":"n","status":"PROVISIONING","ports":["22/tcp","8888/http"],"env":{"B":"2","A":"1"},"cost":0.1}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestCreatePodV2CpuWalksFlavorsByStock(t *testing.T) {
	var posts []map[string]interface{}
	server := createServer(t, []int{http.StatusBadRequest, http.StatusCreated}, &posts)
	defer server.Close()

	pod, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img"})
	if err != nil {
		t.Fatalf("CreatePodV2: %v", err)
	}
	if pod.ID != "new-pod" || pod.DesiredStatus != "RUNNING" {
		t.Fatalf("pod = %+v", *pod)
	}
	var flavors []interface{}
	for _, p := range posts {
		cpu := p["cpu"].(map[string]interface{})
		flavors = append(flavors, cpu["id"])
		if cpu["vcpuCount"] != float64(2) || p["startSsh"] != nil {
			t.Errorf("cpu body = %v", p)
		}
	}
	// most available first; stops at the first success
	if !reflect.DeepEqual(flavors, []interface{}{"cpu5g", "cpu3g"}) {
		t.Fatalf("flavors tried = %v", flavors)
	}
}

func TestCreatePodV2StopsOnBadBody(t *testing.T) {
	var posts []map[string]interface{}
	server := createServer(t, []int{http.StatusUnprocessableEntity}, &posts)
	defer server.Close()

	if _, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n"}); err == nil {
		t.Fatal("expected the 422 to be returned")
	}
	if len(posts) != 1 {
		t.Fatalf("a 422 is not a capacity problem; posts = %d, want 1", len(posts))
	}
}

func TestCreatePodV2GpuBodyAndPlacement(t *testing.T) {
	var posts []map[string]interface{}
	server := createServer(t, nil, &posts)
	defer server.Close()

	pod, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{
		Name: "n", ImageName: "img", CloudType: "SECURE", GpuTypeID: "NVIDIA A40", GpuCount: 2,
		MinCudaVersion: "12.6", ContainerDisk: 30, VolumeInGb: 20, VolumeMountPath: "/workspace",
		Ports: []string{"22/tcp"}, StartSSH: true, CountryCode: "de", Compliance: []string{"GDPR"},
	})
	if err != nil {
		t.Fatalf("CreatePodV2: %v", err)
	}
	got, _ := json.Marshal(posts[0])
	for _, want := range []string{
		`"gpu":{"count":2,"id":"NVIDIA A40","minCudaVersion":"12.6"}`,
		`"mounts":{"persistent":{"path":"/workspace","size":20}}`,
		// country DE gives EU-DE-1/2 for the A40; compliance narrows to EU-DE-2
		`"dataCenterIds":["EU-DE-2"]`,
		`"startSsh":true`, `"cloud":"SECURE"`, `"disk":30`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("body %s missing %s", got, want)
		}
	}
	out := LegacyCreateOutput(pod)
	if out["ports"] != "22/tcp,8888/http" || !reflect.DeepEqual(out["env"], []string{"A=1", "B=2"}) || out["desiredStatus"] != "RUNNING" {
		t.Errorf("legacy output = %v", out)
	}
}

func TestCreatePodV2PlacementWithNoMatchFails(t *testing.T) {
	var posts []map[string]interface{}
	server := createServer(t, nil, &posts)
	defer server.Close()

	_, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{
		Name: "n", GpuTypeID: "NVIDIA A40", CountryCode: "DE", DataCenterIDs: []string{"US-TX-3"},
	})
	if err == nil || !strings.Contains(err.Error(), "no data center matches") || len(posts) != 0 {
		t.Fatalf("err = %v, posts = %d; want a refusal before any create", err, len(posts))
	}
}

// the v2 POST /pods schema has no dockerArgs field, so docker args are sent as
// cmd/entrypoint arrays and never as dockerArgs
func TestCreatePodV2SendsDockerArgsAsCmd(t *testing.T) {
	var posts []map[string]interface{}
	server := createServer(t, nil, &posts)
	defer server.Close()

	cmd, entrypoint := ParseDockerArgs("sleep infinity")
	if _, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{
		Name: "n", ImageName: "img", GpuTypeID: "NVIDIA A40", GpuCount: 1, Cmd: cmd, Entrypoint: entrypoint,
	}); err != nil {
		t.Fatalf("CreatePodV2: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("posts = %v", posts)
	}
	if _, ok := posts[0]["dockerArgs"]; ok {
		t.Errorf("body must not carry dockerArgs: %v", posts[0])
	}
	if !reflect.DeepEqual(posts[0]["cmd"], []interface{}{"sleep", "infinity"}) {
		t.Errorf("cmd = %v", posts[0]["cmd"])
	}
	if _, ok := posts[0]["entrypoint"]; ok {
		t.Errorf("a plain args string sets no entrypoint: %v", posts[0])
	}
}

// a create whose reply cannot be read may still have bought a pod, so the cpu
// walk must stop there rather than try (and maybe buy) the next flavor
func TestCreatePodV2CpuWalkStopsOnUnreadableReply(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/cpus":
			_, _ = io.WriteString(w, `{"cpus":[{"id":"cpu5g","availability":"HIGH"},{"id":"cpu3g","availability":"LOW"}]}`)
		case "/pods":
			posts++
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":`) // truncated
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	if _, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img"}); err == nil {
		t.Fatal("expected the unreadable reply to be an error")
	}
	if posts != 1 {
		t.Errorf("posted %d creates, want 1", posts)
	}
}

// what v2 cannot honour is refused before anything is created
func TestCreatePodV2RefusesUnplaceableRequests(t *testing.T) {
	cases := []struct {
		name string
		req  PodCreateV2Request
		want string
	}{
		{"community country", PodCreateV2Request{CloudType: "COMMUNITY", GpuTypeID: "NVIDIA A40", CountryCode: "US"}, "--country-code is only supported on secure cloud"},
		{"community compliance", PodCreateV2Request{CloudType: "COMMUNITY", GpuTypeID: "NVIDIA A40", Compliance: []string{"GDPR"}}, "--compliance is only supported on secure cloud"},
		{"cpu country", PodCreateV2Request{CloudType: "SECURE", CountryCode: "US"}, "only supported for gpu pods"},
		{"cpu pod volume", PodCreateV2Request{CloudType: "SECURE", VolumeInGb: 20}, "cpu pods cannot have a pod volume"},
		{"pod volume beside a network volume", PodCreateV2Request{CloudType: "SECURE", GpuTypeID: "NVIDIA A40", NetworkVolumeID: "nv1", VolumeInGb: 50}, "--volume-in-gb sets a pod volume"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("no request may be made, got %s %s", r.Method, r.URL.Path)
			}))
			defer server.Close()
			tc.req.Name, tc.req.ImageName = "n", "img"
			_, _, err := newV2TestClient(t, server).CreatePodV2(&tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDefaultPodName(t *testing.T) {
	for _, tc := range []struct {
		req  PodCreateV2Request
		want string
	}{
		{PodCreateV2Request{Name: "mine", ImageName: "alpine:3"}, "mine"},
		{PodCreateV2Request{ImageName: "runpod/base:0.6.2-cuda12.2.0"}, "runpod/base"},
		{PodCreateV2Request{ImageName: "registry.io:5000/team/img"}, "registry.io:5000/team/img"},
		{PodCreateV2Request{ImageName: "registry.io:5000/team/img:v1"}, "registry.io:5000/team/img"},
		{PodCreateV2Request{TemplateID: "tpl-1"}, "tpl-1"},
		{PodCreateV2Request{Name: "  "}, "pod"},
	} {
		if got := defaultPodName(&tc.req); got != tc.want {
			t.Errorf("defaultPodName(%+v) = %q, want %q", tc.req, got, tc.want)
		}
	}
}

// the walk moves on after a 409 like a 400, stops at a 5xx (it may have
// created the pod), and reports the last refusal when every flavor says no
func TestCreatePodV2CpuWalkOutcomes(t *testing.T) {
	t.Run("409 advances", func(t *testing.T) {
		var posts []map[string]interface{}
		server := createServer(t, []int{http.StatusConflict, http.StatusCreated}, &posts)
		defer server.Close()
		if _, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img"}); err != nil || len(posts) != 2 {
			t.Fatalf("err = %v, posts = %d; want the second flavor to succeed", err, len(posts))
		}
	})
	t.Run("5xx stops", func(t *testing.T) {
		var posts []map[string]interface{}
		server := createServer(t, []int{http.StatusBadGateway, http.StatusCreated}, &posts)
		defer server.Close()
		_, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img"})
		var coded interface{ ErrorCode() string }
		if !errors.As(err, &coded) || coded.ErrorCode() != "create_uncertain" || len(posts) != 1 {
			t.Fatalf("err = %v, posts = %d; want one post and create_uncertain", err, len(posts))
		}
	})
	t.Run("all refused", func(t *testing.T) {
		var posts []map[string]interface{}
		server := createServer(t, []int{400, 400, 400}, &posts)
		defer server.Close()
		_, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img"})
		if err == nil || !strings.Contains(err.Error(), "no longer any instances") || len(posts) != 3 {
			t.Fatalf("err = %v, posts = %d; want every flavor tried and the last refusal", err, len(posts))
		}
	})
	t.Run("empty catalog", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/catalog/cpus" {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}
			_, _ = io.WriteString(w, `{"cpus":[]}`)
		}))
		defer server.Close()
		_, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img"})
		if err == nil || !strings.Contains(err.Error(), "no cpu flavors available") {
			t.Fatalf("err = %v, want no cpu flavors available", err)
		}
	})
}

// an accepted create whose reply can't be used carries the pod id as data, so
// an agent can find and delete the pod without parsing the message
func TestCreatePodV2AcceptedButUnusableReplyCarriesID(t *testing.T) {
	for name, body := range map[string]string{
		"unparseable": `{"id":"pod-9","status":12}`,
		"no id":       `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			_, _, err := newV2TestClient(t, server).CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img", GpuTypeID: "NVIDIA A40"})
			var ider interface{ ErrorResourceID() string }
			if !errors.As(err, &ider) || !strings.Contains(err.Error(), "check 'runpodctl pod list' before retrying") {
				t.Fatalf("err = %v, want an accepted-create error", err)
			}
			if want := map[string]string{"unparseable": "pod-9", "no id": ""}[name]; ider.ErrorResourceID() != want {
				t.Fatalf("id = %q, want %q", ider.ErrorResourceID(), want)
			}
		})
	}
}

// no reply at all: the pod may exist, so the error says to look before retrying
func TestCreatePodV2TransportFailureWarns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	client := newV2TestClient(t, server)
	_, _, err := client.CreatePodV2(&PodCreateV2Request{Name: "n", ImageName: "img", GpuTypeID: "NVIDIA A40"})
	if err == nil || !strings.Contains(err.Error(), "the pod may have been created") {
		t.Fatalf("err = %v, want the may-have-been-created hint", err)
	}
	// not network_error, which tells an agent to retry
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "create_uncertain" {
		t.Fatalf("code = %v, want create_uncertain", err)
	}
}
