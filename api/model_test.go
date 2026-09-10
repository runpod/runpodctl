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

func TestAddModelToRepoSendsHuggingFaceMirrorInput(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	var requestInput map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		var ok bool
		requestInput, ok = input.Variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected input variables, got %#v", input.Variables["input"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"addModelToRepo": map[string]interface{}{
					"success": true,
					"model": map[string]interface{}{
						"id": "model-id", "owner": "user-id", "name": "Tiny-LLM", "provider": "LOCAL",
						"versions": []map[string]interface{}{{"hash": "version-hash", "status": "PENDING_TRANSFER"}},
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	model, err := AddModelToRepo(&AddModelToRepoInput{
		Owner:            "destination-owner",
		Name:             "tiny-llm",
		HuggingFaceModel: "arnir0/Tiny-LLM",
		Metadata:         map[string]interface{}{"purpose": "test"},
	})
	if err != nil {
		t.Fatalf("AddModelToRepo returned error: %v", err)
	}
	if got := requestInput["huggingFaceModel"]; got != "arnir0/Tiny-LLM" {
		t.Fatalf("expected huggingFaceModel in request, got %#v", got)
	}
	if got := requestInput["name"]; got != "tiny-llm" {
		t.Fatalf("expected destination name in request, got %#v", got)
	}
	if got := requestInput["owner"]; got != "destination-owner" {
		t.Fatalf("expected destination owner in request, got %#v", got)
	}
	if _, ok := requestInput["provider"]; ok {
		t.Fatalf("did not expect provider in request, got %#v", requestInput)
	}
	metadata, ok := requestInput["metadata"].(map[string]interface{})
	if !ok || metadata["purpose"] != "test" {
		t.Fatalf("expected metadata in request, got %#v", requestInput["metadata"])
	}
	if len(model.Versions) != 1 || model.Versions[0].Hash != "version-hash" || model.Versions[0].Status != "PENDING_TRANSFER" {
		t.Fatalf("expected returned pending transfer version, got %#v", model.Versions)
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

func TestCreateModelRepoUploadBatchSendsAllFilesInOneRequest(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	var requestCount int
	var sentFiles []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var input Input
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		variablesInput, ok := input.Variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected input variables, got %#v", input.Variables["input"])
		}
		files, ok := variablesInput["files"].([]interface{})
		if !ok {
			t.Fatalf("expected files list, got %#v", variablesInput["files"])
		}
		for _, f := range files {
			sentFiles = append(sentFiles, f.(map[string]interface{}))
		}

		uploads := make([]map[string]interface{}, len(files))
		for i, f := range files {
			file := f.(map[string]interface{})
			uploads[i] = map[string]interface{}{
				"sessionId":        "session-" + file["fileName"].(string),
				"uploadId":         "upload-id",
				"bucket":           "bucket",
				"key":              "key-" + file["fileName"].(string),
				"keyPrefix":        "prefix/",
				"partSizeBytes":    5242880,
				"partCount":        1,
				"expiresInSeconds": 3600,
				"parts":            []interface{}{},
				"completeUrl":      "https://example.com/complete",
				"abortUrl":         "https://example.com/abort",
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"createModelRepoUploadBatch": map[string]interface{}{
					"success": true,
					"model": map[string]interface{}{
						"id": "model-id", "owner": "user-id", "name": "test-model", "provider": "LOCAL",
					},
					"version": map[string]interface{}{"uuid": "version-uuid", "hash": "version-hash"},
					"uploads": uploads,
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	result, err := CreateModelRepoUploadBatch(&CreateModelRepoUploadBatchInput{
		Owner: "user-id",
		Name:  "test-model",
		Files: []ModelRepoUploadBatchFileInput{
			{FileName: "a.bin", FileSizeBytes: "1000"},
			{FileName: "b.bin", FileSizeBytes: "2000"},
		},
	})
	if err != nil {
		t.Fatalf("CreateModelRepoUploadBatch returned error: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 HTTP request for the whole batch, got %d", requestCount)
	}
	if len(sentFiles) != 2 {
		t.Fatalf("expected 2 files sent, got %d", len(sentFiles))
	}
	if len(result.Uploads) != 2 {
		t.Fatalf("expected 2 uploads in the response, got %d", len(result.Uploads))
	}
	if result.Uploads[0].SessionID != "session-a.bin" || result.Uploads[1].SessionID != "session-b.bin" {
		t.Fatalf("expected uploads to preserve request order, got %#v", result.Uploads)
	}
	if result.Version.UUID != "version-uuid" {
		t.Fatalf("expected version uuid to decode, got %#v", result.Version)
	}
}

func TestCreateModelRepoUploadBatchRejectsEmptyFiles(t *testing.T) {
	if _, err := CreateModelRepoUploadBatch(&CreateModelRepoUploadBatchInput{Name: "test-model"}); err == nil {
		t.Fatal("expected an error for an empty files list")
	}
}

func TestCreateModelRepoUploadBatchRejectsMismatchedUploadCount(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"createModelRepoUploadBatch": map[string]interface{}{
					"success": true,
					"uploads": []interface{}{
						map[string]interface{}{"sessionId": "only-one"},
					},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := CreateModelRepoUploadBatch(&CreateModelRepoUploadBatchInput{
		Name: "test-model",
		Files: []ModelRepoUploadBatchFileInput{
			{FileName: "a.bin", FileSizeBytes: "1"},
			{FileName: "b.bin", FileSizeBytes: "1"},
		},
	})
	if err == nil {
		t.Fatal("expected an error when the server returns fewer upload sessions than requested files")
	}
}

func TestCreateModelRepoUploadBatchSurfacesGraphQLErrors(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]interface{}{{"message": "Model Repo storage quota exceeded for this namespace"}},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := CreateModelRepoUploadBatch(&CreateModelRepoUploadBatchInput{
		Name:  "test-model",
		Files: []ModelRepoUploadBatchFileInput{{FileName: "a.bin", FileSizeBytes: "1"}},
	})
	if err == nil || !strings.Contains(err.Error(), "storage quota exceeded") {
		t.Fatalf("expected quota exceeded error to surface, got %v", err)
	}
}
