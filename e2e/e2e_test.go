//go:build e2e

package e2e

import (
	"os"
	"testing"

	"github.com/runpod/runpod/internal/api"
	"github.com/spf13/viper"
)

func init() {
	// load config from ~/.runpod/config.toml
	home, _ := os.UserHomeDir()
	viper.AddConfigPath(home + "/.runpod")
	viper.SetConfigType("toml")
	viper.SetConfigName("config")
	viper.ReadInConfig()
}

func TestE2E_APIClient(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create api client: %v", err)
	}
	if client == nil {
		t.Fatal("client is nil")
	}
}

func TestE2E_PodList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	pods, err := client.ListPods(nil)
	if err != nil {
		t.Fatalf("failed to list pods: %v", err)
	}

	t.Logf("found %d pods", len(pods))

	// if we have pods, test getting one
	if len(pods) > 0 {
		pod, err := client.GetPod(pods[0].ID, false, false)
		if err != nil {
			t.Fatalf("failed to get pod %s: %v", pods[0].ID, err)
		}
		if pod.ID != pods[0].ID {
			t.Errorf("expected pod id %s, got %s", pods[0].ID, pod.ID)
		}
		t.Logf("got pod: %s (%s)", pod.Name, pod.ID)
	}
}

func TestE2E_PodListWithOptions(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// test with include machine
	pods, err := client.ListPods(&api.PodListOptions{
		IncludeMachine: true,
	})
	if err != nil {
		t.Fatalf("failed to list pods with machine info: %v", err)
	}
	t.Logf("found %d pods with machine info", len(pods))

	// test with compute type filter
	gpuPods, err := client.ListPods(&api.PodListOptions{
		ComputeType: "GPU",
	})
	if err != nil {
		t.Fatalf("failed to list GPU pods: %v", err)
	}
	t.Logf("found %d GPU pods", len(gpuPods))
}

func TestE2E_EndpointList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	endpoints, err := client.ListEndpoints(nil)
	if err != nil {
		t.Fatalf("failed to list endpoints: %v", err)
	}

	t.Logf("found %d endpoints", len(endpoints))

	// if we have endpoints, test getting one
	if len(endpoints) > 0 {
		endpoint, err := client.GetEndpoint(endpoints[0].ID, false, false)
		if err != nil {
			t.Fatalf("failed to get endpoint %s: %v", endpoints[0].ID, err)
		}
		if endpoint.ID != endpoints[0].ID {
			t.Errorf("expected endpoint id %s, got %s", endpoints[0].ID, endpoint.ID)
		}
		t.Logf("got endpoint: %s (%s)", endpoint.Name, endpoint.ID)
	}
}

func TestE2E_EndpointListWithOptions(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// test with include template
	endpoints, err := client.ListEndpoints(&api.EndpointListOptions{
		IncludeTemplate: true,
	})
	if err != nil {
		t.Fatalf("failed to list endpoints with template: %v", err)
	}
	t.Logf("found %d endpoints with template info", len(endpoints))

	// test with include workers
	endpoints, err = client.ListEndpoints(&api.EndpointListOptions{
		IncludeWorkers: true,
	})
	if err != nil {
		t.Fatalf("failed to list endpoints with workers: %v", err)
	}
	t.Logf("found %d endpoints with worker info", len(endpoints))
}

func TestE2E_TemplateList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	templates, err := client.ListTemplates()
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}

	t.Logf("found %d templates", len(templates))

	// if we have templates, test getting one
	if len(templates) > 0 {
		template, err := client.GetTemplate(templates[0].ID)
		if err != nil {
			t.Fatalf("failed to get template %s: %v", templates[0].ID, err)
		}
		if template.ID != templates[0].ID {
			t.Errorf("expected template id %s, got %s", templates[0].ID, template.ID)
		}
		t.Logf("got template: %s (%s)", template.Name, template.ID)
	}
}

func TestE2E_VolumeList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	volumes, err := client.ListNetworkVolumes()
	if err != nil {
		t.Fatalf("failed to list volumes: %v", err)
	}

	t.Logf("found %d volumes", len(volumes))

	// if we have volumes, test getting one
	if len(volumes) > 0 {
		volume, err := client.GetNetworkVolume(volumes[0].ID)
		if err != nil {
			t.Fatalf("failed to get volume %s: %v", volumes[0].ID, err)
		}
		if volume.ID != volumes[0].ID {
			t.Errorf("expected volume id %s, got %s", volumes[0].ID, volume.ID)
		}
		t.Logf("got volume: %s (%s) - %dGB in %s", volume.Name, volume.ID, volume.Size, volume.DataCenterID)
	}
}

func TestE2E_RegistryList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	auths, err := client.ListContainerRegistryAuths()
	if err != nil {
		t.Fatalf("failed to list registry auths: %v", err)
	}

	t.Logf("found %d registry auths", len(auths))

	// if we have auths, test getting one
	if len(auths) > 0 {
		auth, err := client.GetContainerRegistryAuth(auths[0].ID)
		if err != nil {
			t.Fatalf("failed to get registry auth %s: %v", auths[0].ID, err)
		}
		if auth.ID != auths[0].ID {
			t.Errorf("expected auth id %s, got %s", auths[0].ID, auth.ID)
		}
		t.Logf("got registry auth: %s (%s)", auth.Name, auth.ID)
	}
}

func TestE2E_User(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	user, err := client.GetUser()
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	if user.ID == "" {
		t.Error("expected user id")
	}
	t.Logf("user: %s, balance: $%.2f, spend/hr: $%.2f", user.Email, user.ClientBalance, user.CurrentSpendPerHr)
}

func TestE2E_GpuList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	gpus, err := client.ListGpuTypes(false)
	if err != nil {
		t.Fatalf("failed to list gpus: %v", err)
	}

	if len(gpus) == 0 {
		t.Error("expected at least one gpu type")
	}
	t.Logf("found %d available gpu types", len(gpus))

	// check that we filtered out unavailable ones
	for _, gpu := range gpus {
		if !gpu.Available && gpu.StockStatus == "" {
			t.Errorf("gpu %s should have been filtered out", gpu.ID)
		}
	}
}

func TestE2E_GpuListIncludeUnavailable(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	gpusAll, err := client.ListGpuTypes(true)
	if err != nil {
		t.Fatalf("failed to list all gpus: %v", err)
	}

	gpusAvailable, err := client.ListGpuTypes(false)
	if err != nil {
		t.Fatalf("failed to list available gpus: %v", err)
	}

	// including unavailable should return more or equal GPUs
	if len(gpusAll) < len(gpusAvailable) {
		t.Errorf("expected all gpus (%d) >= available gpus (%d)", len(gpusAll), len(gpusAvailable))
	}
	t.Logf("all gpus: %d, available gpus: %d", len(gpusAll), len(gpusAvailable))
}

func TestE2E_DataCenterList(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	dataCenters, err := client.ListDataCenters()
	if err != nil {
		t.Fatalf("failed to list datacenters: %v", err)
	}

	if len(dataCenters) == 0 {
		t.Error("expected at least one datacenter")
	}
	t.Logf("found %d datacenters", len(dataCenters))

	// check that we have gpu availability info
	hasAvailability := false
	for _, dc := range dataCenters {
		if len(dc.GpuAvailability) > 0 {
			hasAvailability = true
			break
		}
	}
	if !hasAvailability {
		t.Error("expected at least one datacenter with gpu availability")
	}
}

func TestE2E_BillingPods(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	records, err := client.GetPodBilling(nil)
	if err != nil {
		t.Fatalf("failed to get pod billing: %v", err)
	}

	t.Logf("found %d pod billing records", len(records))
}

func TestE2E_BillingEndpoints(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	records, err := client.GetEndpointBilling(nil)
	if err != nil {
		t.Fatalf("failed to get endpoint billing: %v", err)
	}

	t.Logf("found %d endpoint billing records", len(records))
}

func TestE2E_BillingNetworkVolumes(t *testing.T) {
	client, err := api.NewClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	records, err := client.GetNetworkVolumeBilling(nil)
	if err != nil {
		t.Fatalf("failed to get network volume billing: %v", err)
	}

	t.Logf("found %d network volume billing records", len(records))
}
