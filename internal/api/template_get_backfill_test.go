package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
)

// The REST template schema has no port labels, so GetTemplate must backfill
// portsConfig from GraphQL when REST returns ports but no labels.
func TestGetTemplateBackfillsPortsConfigFromGraphQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// REST: ports present, portsConfig absent (not in the REST schema).
			_ = json.NewEncoder(w).Encode(Template{
				ID:    "tpl-1",
				Name:  "t",
				Ports: []string{"22/tcp", "8888/http"},
			})
			return
		}
		// GraphQL: returns the labels.
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"podTemplate": map[string]interface{}{
					"id":          "tpl-1",
					"name":        "t",
					"ports":       "22/tcp,8888/http",
					"portsConfig": []map[string]string{{"port": "22", "name": "ssh"}, {"port": "8888", "name": "jupyter"}},
				},
			},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")
	viper.Set("apiUrl", server.URL)
	defer viper.Set("apiUrl", "")

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.baseURL = server.URL

	tmpl, err := client.GetTemplate("tpl-1")
	if err != nil {
		t.Fatalf("GetTemplate() error = %v", err)
	}
	if len(tmpl.PortsConfig) != 2 {
		t.Fatalf("expected 2 backfilled port labels, got %d (%#v)", len(tmpl.PortsConfig), tmpl.PortsConfig)
	}
	if tmpl.PortsConfig[0].Port != "22" || tmpl.PortsConfig[0].Name != "ssh" {
		t.Fatalf("unexpected first label: %#v", tmpl.PortsConfig[0])
	}
}
