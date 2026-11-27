package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	runpod "github.com/runpod/go-sdk/v2"
	sdkpods "github.com/runpod/go-sdk/v2/lib/pods"
	"github.com/runpod/go-sdk/v2/lib/types"
)

// v2Pod is a pod as rest v2 reports it. toPod maps it onto the field names the
// cli has always printed; toLegacyPod builds the runtime/ssh view that
// internal/sshconnect and internal/podstate consume.
//
// v2 has no lastStatusChange and no machine details, so those are not reported
// (`--include-machine` carries only dataCenterId and secureCloud).
// legacyDesiredStatus maps v2's observed status onto the requested-state value
// the cli has always printed as desiredStatus: a pod being provisioned or
// started was asked to run.
func legacyDesiredStatus(status string) string {
	switch strings.ToUpper(status) {
	// ERROR is v2's "unrecoverable" state and is passed through, so a wait
	// ends instead of treating a dead pod as one still booting
	case "PROVISIONING", "STARTING", "RUNNING":
		return "RUNNING"
	default:
		return strings.ToUpper(status)
	}
}

// legacyTimestamp renders a v2 time the way v1 did (Go's time.String layout,
// e.g. "2026-10-02 05:28:38.366 +0000 UTC"), which `pod get` has always
// printed. A missing time is left out.
func legacyTimestamp(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t.UTC().String()
}

// isPublicIP reports whether ip is reachable from the internet. graphql flagged
// this per port (isIpPublic); v2 does not, and its http ports report an address
// on the runpod proxy's carrier-grade nat range.
func isPublicIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() ||
		parsed.IsUnspecified() || parsed.IsMulticast() {
		return false
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return !cgnat.Contains(parsed)
}

// parseSDKPod decodes a rest v2 pod read.
func parseSDKPod(data []byte) (*Pod, error) {
	var p types.Pod
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	pod := podFromSDK(p)
	return &pod, nil
}

// listSDKPods reads every pod, following v2's pagination.
func (c *Client) listSDKPods() ([]types.Pod, error) {
	var pods []types.Pod
	err := sdkAllPages("/pods", func(cursor *string) (types.Pagination, error) {
		page, err := sdkCall[types.ListPodsResponse](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
			return sdk.Pods.ListPods(ctx, &sdkpods.ListPodsParams{Cursor: cursor})
		})
		if err != nil {
			return types.Pagination{}, err
		}
		pods = append(pods, page.Pods...)
		return page.Pagination, nil
	})
	return pods, err
}

// getSDKPod reads one pod.
func (c *Client) getSDKPod(podID string) (*types.Pod, error) {
	if err := requireID(podID); err != nil {
		return nil, err
	}
	return sdkCall[types.Pod](c, func(ctx context.Context, sdk *runpod.Client) (*http.Response, error) {
		return sdk.Pods.GetPod(ctx, podID)
	})
}

// GetPods returns every pod in the runtime/ssh view that internal/sshconnect,
// internal/podstate and the --wait poll consume. It replaces the graphql
// myPods read: v2 reports runtime telemetry and ports directly.
func (c *Client) GetPods() ([]*LegacyPod, error) {
	pods, err := c.listSDKPods()
	if err != nil {
		return nil, err
	}
	out := make([]*LegacyPod, 0, len(pods))
	for _, p := range pods {
		out = append(out, legacyPodFromSDK(p))
	}
	return out, nil
}

// GetPodRuntime reads one pod by id in the same runtime/ssh view as GetPods.
// The v2 list can leave runtime out while the single-pod read reports it
// (seen in prod), so a wait that needs runtime fills it in from here.
func (c *Client) GetPodRuntime(podID string) (*LegacyPod, error) {
	p, err := c.getSDKPod(podID)
	if err != nil {
		return nil, err
	}
	return legacyPodFromSDK(*p), nil
}

// GetLegacyPods returns every pod as the legacy `get pod` command prints it:
// the runtime view plus the graphql machine block and podType. v2 reports neither, so the gpu display name comes from the catalog
// and the location from the data center id (legacyLocation). v2 cannot tell a
// spot pod from an on-demand one, so podType is always RESERVED.
func (c *Client) GetLegacyPods() ([]*LegacyPod, error) {
	pods, err := c.listSDKPods()
	if err != nil {
		return nil, err
	}
	var gpuNames map[string]string
	out := make([]*LegacyPod, 0, len(pods))
	for _, p := range pods {
		legacy := legacyPodFromSDK(p)
		legacy.PodType = "RESERVED"
		machine := &LegacyMachine{GpuDisplayName: "unknown", Location: legacyLocation(deref(p.DataCenterId))}
		if p.Gpu != nil && p.Gpu.Id != "" {
			if gpuNames == nil {
				gpuNames = c.catalogGpuNames()
			}
			machine.GpuDisplayName = p.Gpu.Id
			if name := gpuNames[p.Gpu.Id]; name != "" {
				machine.GpuDisplayName = name
			}
		}
		legacy.Machine = machine
		out = append(out, legacy)
	}
	return out, nil
}

// catalogGpuNames maps gpu type ids to the display names graphql reported as
// machine.gpuDisplayName (the catalog's name is the same string). best-effort:
// on failure the caller falls back to the gpu type id.
func (c *Client) catalogGpuNames() map[string]string {
	names := map[string]string{}
	gpus, err := c.listCatalogGpus(nil)
	if err != nil {
		return names
	}
	for _, gpu := range gpus {
		names[gpu.Id] = gpu.Name
	}
	return names
}

// legacyLocation approximates graphql's machine.location, a country code, from
// a data center id: "US-NC-2" -> "US", "EU-RO-1" -> "RO". ids that lead with a
// region rather than a country take the second segment.
func legacyLocation(dataCenterID string) string {
	parts := strings.Split(dataCenterID, "-")
	if len(parts) < 2 {
		return dataCenterID
	}
	switch parts[0] {
	case "EU", "EUR", "AP", "OC", "SEA", "SA", "AF", "ME":
		return parts[1]
	}
	return parts[0]
}
