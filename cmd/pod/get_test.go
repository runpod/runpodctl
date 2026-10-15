package pod

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
	"github.com/spf13/cobra"
)

func TestRunGetPrintsIncludedNetworkVolume(t *testing.T) {
	// v2 reports only the mounted volume's id; the cli reads the volume itself
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/pods/pod-123":
			_, _ = w.Write([]byte(`{"id":"pod-123","name":"my-pod","status":"EXITED",
				"mounts":{"network":[{"volumeId":"vol-123","path":"/workspace"}]}}`))
		case "/network-volumes/vol-123":
			_, _ = w.Write([]byte(`{"id":"vol-123","name":"my-volume","dataCenter":"US-TX-1","size":10}`))
		case "/account/ssh-keys":
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	t.Setenv("RUNPOD_API_URL", "http://v1.invalid")
	t.Setenv("RUNPOD_REST_V2_URL", server.URL)
	t.Setenv("RUNPOD_GRAPHQL_URL", "http://graphql.invalid")

	oldIncludeMachine := getIncludeMachine
	oldIncludeNetworkVolume := getIncludeNetworkVolume
	getIncludeMachine = false
	getIncludeNetworkVolume = true
	t.Cleanup(func() {
		getIncludeMachine = oldIncludeMachine
		getIncludeNetworkVolume = oldIncludeNetworkVolume
	})

	cmd := &cobra.Command{}
	cmd.Flags().String("output", "json", "")

	printed, err := capturePodGetStdout(func() error {
		return runGet(cmd, []string{"pod-123"})
	})
	if err != nil {
		t.Fatalf("run pod get: %v", err)
	}

	var got struct {
		NetworkVolumeID string             `json:"networkVolumeId"`
		NetworkVolume   *api.NetworkVolume `json:"networkVolume"`
	}
	if err := json.Unmarshal(printed, &got); err != nil {
		t.Fatalf("decode pod get output: %v\noutput: %s", err, printed)
	}
	if got.NetworkVolumeID != "vol-123" {
		t.Errorf("expected networkVolumeId vol-123, got %q", got.NetworkVolumeID)
	}
	if got.NetworkVolume == nil {
		t.Fatalf("network volume missing from pod get output: %s", printed)
	}
	if got.NetworkVolume.ID != "vol-123" {
		t.Errorf("expected volume id vol-123, got %q", got.NetworkVolume.ID)
	}
	if got.NetworkVolume.Name != "my-volume" {
		t.Errorf("expected volume name my-volume, got %q", got.NetworkVolume.Name)
	}
	if got.NetworkVolume.DataCenterID != "US-TX-1" {
		t.Errorf("expected data center US-TX-1, got %q", got.NetworkVolume.DataCenterID)
	}
	if got.NetworkVolume.Size != 10 {
		t.Errorf("expected volume size 10, got %d", got.NetworkVolume.Size)
	}
}

func capturePodGetStdout(run func() error) ([]byte, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	original := os.Stdout
	os.Stdout = writer
	runErr := run()
	closeErr := writer.Close()
	os.Stdout = original

	printed, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if runErr != nil {
		return printed, runErr
	}
	if closeErr != nil {
		return printed, closeErr
	}
	return printed, readErr
}
