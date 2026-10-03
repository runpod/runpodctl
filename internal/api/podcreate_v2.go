package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"github.com/runpod/runpodctl/internal/clierr"
)

// PodCreateV2Request is a pod create, in the cli's own terms. CreatePodV2 turns
// it into the rest v2 body.
type PodCreateV2Request struct {
	Name             string
	ImageName        string
	TemplateID       string
	CloudType        string // SECURE or COMMUNITY
	GpuTypeID        string // empty for a cpu pod
	GpuCount         int
	MinCudaVersion   string
	ContainerDisk    int
	VolumeInGb       int
	VolumeMountPath  string
	NetworkVolumeID  string
	Ports            []string
	Env              map[string]string
	Cmd              []string
	Entrypoint       []string
	DataCenterIDs    []string
	RegistryAuthID   string
	StartSSH         bool
	GlobalNetworking bool
	CountryCode      string
	Compliance       []string
}

type v2PodGpu struct {
	ID             string `json:"id"`
	Count          int    `json:"count,omitempty"`
	MinCudaVersion string `json:"minCudaVersion,omitempty"`
}

type v2PodCpu struct {
	ID        string `json:"id"`
	VcpuCount int    `json:"vcpuCount"`
}

type v2PodCreate struct {
	Name             string            `json:"name"`
	Image            string            `json:"image,omitempty"`
	TemplateID       string            `json:"templateId,omitempty"`
	Cloud            string            `json:"cloud,omitempty"`
	Gpu              *v2PodGpu         `json:"gpu,omitempty"`
	Cpu              *v2PodCpu         `json:"cpu,omitempty"`
	Disk             int               `json:"disk,omitempty"`
	Mounts           *v2PodMounts      `json:"mounts,omitempty"`
	Ports            []string          `json:"ports,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	Cmd              []string          `json:"cmd,omitempty"`
	Entrypoint       []string          `json:"entrypoint,omitempty"`
	DataCenterIDs    []string          `json:"dataCenterIds,omitempty"`
	Registry         string            `json:"registry,omitempty"`
	StartSSH         bool              `json:"startSsh,omitempty"`
	GlobalNetworking bool              `json:"globalNetworking,omitempty"`
}

// v1DefaultCpuVcpus is the vcpu count v1 gave a cpu pod created without one.
const v1DefaultCpuVcpus = 2

var stockOrderV2 = map[string]int{"HIGH": 0, "MEDIUM": 1, "LOW": 2, "NONE": 3}

// CreatePodV2 creates a pod over rest v2 and returns the raw v2 pod.
//
// A cpu pod names no flavor, and v1 walked its list of cpu flavors server-side;
// v2 creates exactly the flavor it is given, so the walk happens here, most
// available flavor first. A 422 is a bad body, not scarce capacity, so it ends
// the walk at once.
func (c *Client) CreatePodV2(req *PodCreateV2Request) (*Pod, map[string]interface{}, error) {
	if err := checkPlacement(req); err != nil {
		return nil, nil, err
	}
	body := &v2PodCreate{
		Name:             req.Name,
		Image:            req.ImageName,
		TemplateID:       req.TemplateID,
		Cloud:            req.CloudType,
		Disk:             req.ContainerDisk,
		Ports:            req.Ports,
		Env:              req.Env,
		Cmd:              req.Cmd,
		Entrypoint:       req.Entrypoint,
		Registry:         req.RegistryAuthID,
		StartSSH:         req.StartSSH,
		GlobalNetworking: req.GlobalNetworking,
	}
	switch {
	case req.NetworkVolumeID != "":
		path := req.VolumeMountPath
		if path == "" {
			path = "/workspace"
		}
		body.Mounts = &v2PodMounts{Network: []v2NetworkMount{{VolumeID: req.NetworkVolumeID, Path: path}}}
	case req.VolumeInGb > 0:
		body.Mounts = &v2PodMounts{Persistent: &v2PersistentMount{Size: req.VolumeInGb, Path: req.VolumeMountPath}}
	}

	dataCenters, err := c.placementDataCenters(req)
	if err != nil {
		return nil, nil, err
	}
	body.DataCenterIDs = dataCenters

	if req.GpuTypeID != "" {
		body.Gpu = &v2PodGpu{ID: req.GpuTypeID, Count: req.GpuCount, MinCudaVersion: req.MinCudaVersion}
		return c.postPod(body)
	}

	flavors, err := c.cpuFlavorsByStock()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read cpu flavors: %w", err)
	}
	var lastErr error
	for _, flavor := range flavors {
		body.Cpu = &v2PodCpu{ID: flavor, VcpuCount: v1DefaultCpuVcpus}
		pod, raw, err := c.postPod(body)
		if err == nil {
			return pod, raw, nil
		}
		// only a clean refusal moves on to the next flavor. a timeout or an
		// unreadable reply may mean the pod was created, and trying another
		// flavor would buy a second one.
		var apiErr *APIError
		if !errors.As(err, &apiErr) || (apiErr.Status != 400 && apiErr.Status != 409) {
			return nil, nil, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no cpu flavors available")
	}
	return nil, nil, lastErr
}

// checkPlacement refuses, before anything is created, what v2 cannot honour.
// --country-code resolves through the gpu catalog, and community stock has no
// data center to resolve to; a cpu pod cannot have a persistent volume.
func checkPlacement(req *PodCreateV2Request) error {
	community := strings.EqualFold(req.CloudType, "COMMUNITY")
	switch {
	case community && req.CountryCode != "":
		return clierr.Usagef("--country-code is only supported on secure cloud")
	case community && len(cleanList(req.Compliance)) > 0:
		return clierr.Usagef("--compliance is only supported on secure cloud")
	case req.GpuTypeID == "" && req.CountryCode != "":
		return clierr.Usagef("--country-code is only supported for gpu pods; use --data-center-ids")
	case req.GpuTypeID == "" && req.VolumeInGb > 0 && req.NetworkVolumeID == "":
		return clierr.Usagef("cpu pods cannot have a pod volume; use --network-volume-id")
	}
	return nil
}

func (c *Client) postPod(body *v2PodCreate) (*Pod, map[string]interface{}, error) {
	data, err := c.PostV2("/pods", body)
	if err != nil {
		return nil, nil, err
	}
	var raw map[string]interface{}
	_ = json.Unmarshal(data, &raw)
	pod, err := parseV2Pod(data)
	if err != nil {
		// the create was accepted, so a pod may exist and bill: say so, or the
		// obvious next step (running the create again) buys a second one
		id, _ := raw["id"].(string)
		if id == "" {
			id = "unknown"
		}
		return nil, raw, fmt.Errorf("the create was accepted but its reply could not be read (%v); pod id %s; check 'runpodctl pod list' before retrying", err, id)
	}
	return pod, raw, nil
}

func (c *Client) cpuFlavorsByStock() ([]string, error) {
	data, err := c.GetV2("/catalog/cpus", url.Values{
		"include":   {"AVAILABILITY"},
		"product":   {"POD"},
		"vcpuCount": {strconv.Itoa(v1DefaultCpuVcpus)},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Cpus []struct {
			ID           string `json:"id"`
			Availability string `json:"availability"`
		} `json:"cpus"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	sort.SliceStable(resp.Cpus, func(i, j int) bool {
		return stockOrderV2[strings.ToUpper(resp.Cpus[i].Availability)] < stockOrderV2[strings.ToUpper(resp.Cpus[j].Availability)]
	})
	ids := make([]string, 0, len(resp.Cpus))
	for _, cpu := range resp.Cpus {
		ids = append(ids, cpu.ID)
	}
	return ids, nil
}

// placementDataCenters turns --data-center-ids, --country-code and
// --compliance into v2's dataCenterIds, which the scheduler enforces. v1 and
// graphql took country and compliance as create fields; v2 resolves them
// through the catalog. A filter that leaves nothing is an error rather than an
// unconstrained create.
func (c *Client) placementDataCenters(req *PodCreateV2Request) ([]string, error) {
	allowed := cleanList(req.DataCenterIDs)
	narrow := func(candidates []string, what string) error {
		if len(allowed) > 0 {
			candidates = intersect(allowed, candidates)
		}
		if len(candidates) == 0 {
			return fmt.Errorf("no data center matches %s", what)
		}
		allowed = candidates
		return nil
	}

	if req.CountryCode != "" {
		params := url.Values{"include": {"AVAILABILITY"}, "product": {"POD"}, "countryCodes": {strings.ToUpper(req.CountryCode)}}
		gpus, err := c.listCatalogGpus(params)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve --country-code: %w", err)
		}
		var ids []string
		for _, gpu := range gpus {
			if req.GpuTypeID != "" && !strings.EqualFold(gpu.ID, req.GpuTypeID) {
				continue
			}
			for _, dc := range gpu.DataCenters {
				ids = append(ids, dc.ID)
			}
		}
		if err := narrow(dedupe(ids), "--country-code "+req.CountryCode); err != nil {
			return nil, err
		}
	}

	if len(req.Compliance) > 0 {
		data, err := c.GetV2("/catalog/datacenters", url.Values{"compliance": {strings.Join(cleanList(req.Compliance), ",")}})
		if err != nil {
			return nil, fmt.Errorf("failed to resolve --compliance: %w", err)
		}
		var resp struct {
			DataCenters []struct {
				ID string `json:"id"`
			} `json:"dataCenters"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		var ids []string
		for _, dc := range resp.DataCenters {
			ids = append(ids, dc.ID)
		}
		if err := narrow(ids, "--compliance "+strings.Join(req.Compliance, ",")); err != nil {
			return nil, err
		}
	}
	return allowed, nil
}

func cleanList(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, v := range b {
		in[strings.ToUpper(v)] = true
	}
	var out []string
	for _, v := range a {
		if in[strings.ToUpper(v)] {
			out = append(out, v)
		}
	}
	return out
}

// LegacyCreateOutput renders a created pod in the shape the graphql create
// printed for gpu pods: ports as one comma-joined string and env as KEY=VALUE
// entries. v2 has no machine or lastStatusChange, so those are absent.
func LegacyCreateOutput(pod *Pod) map[string]interface{} {
	env := []string{}
	for k, v := range pod.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return map[string]interface{}{
		"id":                pod.ID,
		"name":              pod.Name,
		"imageName":         pod.ImageName,
		"desiredStatus":     pod.DesiredStatus,
		"costPerHr":         pod.CostPerHr,
		"containerDiskInGb": pod.ContainerDiskInGb,
		"volumeInGb":        pod.VolumeInGb,
		"volumeMountPath":   pod.VolumeMountPath,
		"gpuCount":          pod.GpuCount,
		"memoryInGb":        pod.MemoryInGb,
		"vcpuCount":         pod.VcpuCount,
		"ports":             strings.Join(pod.Ports, ","),
		"env":               env,
	}
}

// ParseDockerArgs converts a docker args string (--docker-args, legacy --args) into the dockerStartCmd /
// dockerEntrypoint arrays the REST API expects (its schema has no dockerArgs
// field and rejects it as an extra key). It mirrors the backend's decoding of
// legacy dockerArgs strings so the flag means the same thing on the GraphQL
// and REST paths: a JSON `{"cmd":[...],"entrypoint":[...]}` object (the
// backend's canonical encoding, also produced by template create) is used
// as-is, anything else is shlex-split into the start cmd, falling back to a
// whitespace split when the shell lexer fails (e.g. unbalanced quotes).
func ParseDockerArgs(args string) (cmd, entrypoint []string) {
	var parsed struct {
		Cmd        []string `json:"cmd"`
		Entrypoint []string `json:"entrypoint"`
	}
	if err := json.Unmarshal([]byte(args), &parsed); err == nil {
		return parsed.Cmd, parsed.Entrypoint
	}
	tokens, err := shlex.Split(args)
	if err != nil {
		return strings.Fields(args), nil
	}
	return tokens, nil
}

// LegacyPodCreate holds the flags of the deprecated `create pod` and `create
// pods` commands, which graphql's podFindAndDeployOnDemand took.
type LegacyPodCreate struct {
	CloudType         string
	ContainerDiskInGb int
	DockerArgs        string
	DataCenterID      string
	Env               map[string]string
	GpuCount          int
	GpuTypeID         string
	ImageName         string
	Name              string
	NetworkVolumeID   string
	Ports             []string
	StartSSH          bool
	TemplateID        string
	VolumeInGb        int
	VolumeMountPath   string
}

// MinPodVolumeInGb is the smallest persistent volume rest v2 creates; graphql
// took any size, and the legacy commands default to 1.
const MinPodVolumeInGb = 10

// V2Request maps the legacy create onto a v2 create. an empty name defaults to
// the image name without its tag, as the legacy create always did, and a
// volume below v2's minimum is raised to it so the legacy mount still exists.
func (l *LegacyPodCreate) V2Request() *PodCreateV2Request {
	name := l.Name
	if name == "" {
		name = strings.Split(l.ImageName, ":")[0]
	}
	req := &PodCreateV2Request{
		Name:            name,
		ImageName:       l.ImageName,
		TemplateID:      l.TemplateID,
		CloudType:       l.CloudType,
		ContainerDisk:   l.ContainerDiskInGb,
		VolumeInGb:      l.VolumeInGb,
		VolumeMountPath: l.VolumeMountPath,
		NetworkVolumeID: l.NetworkVolumeID,
		Ports:           l.Ports,
		Env:             l.Env,
		StartSSH:        l.StartSSH,
	}
	if req.VolumeInGb > 0 && req.VolumeInGb < MinPodVolumeInGb {
		req.VolumeInGb = MinPodVolumeInGb
	}
	if l.GpuTypeID != "" {
		req.GpuTypeID = l.GpuTypeID
		req.GpuCount = l.GpuCount
	}
	if l.DataCenterID != "" {
		req.DataCenterIDs = []string{l.DataCenterID}
	}
	if l.DockerArgs != "" {
		req.Cmd, req.Entrypoint = ParseDockerArgs(l.DockerArgs)
	}
	return req
}
