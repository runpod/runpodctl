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
