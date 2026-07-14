package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestAddModelToRepoSendsProviderWhenProvided(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	var provider string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		variablesInput, ok := input.Variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected input variables, got %#v", input.Variables["input"])
		}
		if value, ok := variablesInput["provider"].(string); ok {
			provider = value
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"addModelToRepo": map[string]interface{}{
					"success": true,
					"model": map[string]interface{}{
						"id":       "model-id",
						"owner":    "user-id",
						"name":     "test-model",
						"provider": "LOCAL",
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	model, err := AddModelToRepo(&AddModelToRepoInput{
		Owner:    "user-id",
		Name:     "test-model",
		Provider: "LOCAL",
	})
	if err != nil {
		t.Fatalf("AddModelToRepo returned error: %v", err)
	}
	if provider != "LOCAL" {
		t.Fatalf("expected provider LOCAL in request, got %q", provider)
	}
	if model.Provider != "LOCAL" {
		t.Fatalf("expected response provider LOCAL, got %q", model.Provider)
	}
}

func TestGetModelsRequestsVersionIdentifiers(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		query = input.Query

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"myModels": []map[string]interface{}{
					{
						"id":       "model-id",
						"owner":    "user-id",
						"name":     "test-model",
						"provider": "LOCAL",
						"versions": []map[string]interface{}{
							{
								"uuid": "version-uuid",
								"hash": "version-hash",
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	models, err := GetModels(&GetModelsInput{Name: "test-model"})
	if err != nil {
		t.Fatalf("GetModels returned error: %v", err)
	}
	if !strings.Contains(query, "uuid") {
		t.Fatalf("expected GetModels query to request version uuid, got %s", query)
	}
	if !strings.Contains(query, "hash") {
		t.Fatalf("expected GetModels query to request version hash, got %s", query)
	}
	if strings.Contains(query, "versionHash") {
		t.Fatalf("GetModels query must not request versionHash, got %s", query)
	}
	if len(models) != 1 || len(models[0].Versions) != 1 {
		t.Fatalf("expected one model version, got %#v", models)
	}
	version := models[0].Versions[0]
	if version.UUID != "version-uuid" || version.Hash != "version-hash" {
		t.Fatalf("expected version identifiers to decode, got %#v", version)
	}
}

func TestUpdateModelVersionStatusByIdentifierUsesUUID(t *testing.T) {
	input, err := newUpdateModelVersionStatusInput(&UpdateModelVersionStatusInput{
		UUID:   "version-uuid",
		Status: ModelVersionStatusPodRemoved,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.Variables["uuid"] != "version-uuid" {
		t.Fatalf("expected uuid variable, got %#v", input.Variables)
	}
	if _, ok := input.Variables["hash"]; ok {
		t.Fatalf("did not expect hash variable, got %#v", input.Variables)
	}
	if input.Variables["status"] != ModelVersionStatusPodRemoved {
		t.Fatalf("expected status %q, got %#v", ModelVersionStatusPodRemoved, input.Variables["status"])
	}
}

func TestUpdateModelVersionStatusByIdentifierUsesHash(t *testing.T) {
	input, err := newUpdateModelVersionStatusInput(&UpdateModelVersionStatusInput{
		Hash:   "version-hash",
		Status: ModelVersionStatusPodRemoved,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.Variables["hash"] != "version-hash" {
		t.Fatalf("expected hash variable, got %#v", input.Variables)
	}
	if _, ok := input.Variables["uuid"]; ok {
		t.Fatalf("did not expect uuid variable, got %#v", input.Variables)
	}
}

func TestUpdateModelVersionStatusByIdentifierValidatesIdentifier(t *testing.T) {
	if _, err := newUpdateModelVersionStatusInput(&UpdateModelVersionStatusInput{Status: ModelVersionStatusPodRemoved}); err == nil {
		t.Fatal("expected missing identifier error")
	}
	if _, err := newUpdateModelVersionStatusInput(&UpdateModelVersionStatusInput{
		UUID:   "version-uuid",
		Hash:   "version-hash",
		Status: ModelVersionStatusPodRemoved,
	}); err == nil {
		t.Fatal("expected conflicting identifier error")
	}
}
