package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/spf13/viper"
)

// TestModelRepoFunctionsReturnGraphQLErrorOnAccessDenied pins the STO-357
// contract: when Model Repo is disabled/inaccessible, the backend returns a
// 200 with a top-level `errors` array and no data (see
// assertModelRepoFeatureEnabled in node/graphql/schema/modelRepo.ts). Every
// model.go function must wrap that as a *internalapi.GraphQLError (code
// "graphql_error") instead of a bare errors.New, so the code is reliable for
// automation.
func TestModelRepoFunctionsReturnGraphQLErrorOnAccessDenied(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	const disabledMessage = "Model Repo feature is not enabled for this user"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": nil,
			"errors": []map[string]interface{}{
				{"message": disabledMessage, "extensions": map[string]interface{}{"code": "RUNPOD"}},
			},
		})
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	tests := []struct {
		name string
		call func() error
	}{
		{"AddModelToRepo", func() error {
			_, err := AddModelToRepo(&AddModelToRepoInput{Name: "m"})
			return err
		}},
		{"GetModels", func() error {
			_, err := GetModels(&GetModelsInput{})
			return err
		}},
		{"GetModel", func() error {
			_, err := GetModel(&GetModelInput{Owner: "o", Name: "n"})
			return err
		}},
		{"RemoveModel", func() error {
			_, err := RemoveModel(&RemoveModelInput{Owner: "o", Name: "n"})
			return err
		}},
		{"CreateModelRepoUpload", func() error {
			_, err := CreateModelRepoUpload(&CreateModelRepoUploadInput{Name: "m", FileName: "f.bin", FileSizeBytes: "10"})
			return err
		}},
		{"CreateModelRepoUploadBatch", func() error {
			_, err := CreateModelRepoUploadBatch(&CreateModelRepoUploadBatchInput{
				Name:  "m",
				Files: []ModelRepoUploadBatchFileInput{{FileName: "f.bin", FileSizeBytes: "10"}},
			})
			return err
		}},
		{"GetModelRepoStorageUsage", func() error {
			_, err := GetModelRepoStorageUsage("o")
			return err
		}},
		{"CompleteModelRepoUpload", func() error {
			_, err := CompleteModelRepoUpload("session-id")
			return err
		}},
		{"UpdateModelVersionStatusByIdentifier", func() error {
			_, err := UpdateModelVersionStatusByIdentifier(&UpdateModelVersionStatusInput{Hash: "h", Status: ModelVersionStatusReady})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("expected an error when Model Repo access is denied; a nil error means the cli would exit 0")
			}

			var gqlErr *internalapi.GraphQLError
			if !errors.As(err, &gqlErr) {
				t.Fatalf("expected a *internalapi.GraphQLError so the code is stable, got %#v (%v)", err, err)
			}
			if gqlErr.ErrorCode() != "graphql_error" {
				t.Fatalf("expected code graphql_error, got %q", gqlErr.ErrorCode())
			}
			if gqlErr.Message != disabledMessage {
				t.Fatalf("expected the backend message to survive unwrapped, got %q", gqlErr.Message)
			}
		})
	}
}

// TestModelRepoFunctionsReturnGraphQLErrorOnHTTPFailure pins the other half
// of STO-357: a non-200 response from the graphql endpoint (e.g. the api
// gateway rejecting the request) must also carry a stable code and the http
// status.
func TestModelRepoFunctionsReturnGraphQLErrorOnHTTPFailure(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", "test-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("model repo backend unavailable"))
	}))
	defer server.Close()
	t.Setenv("RUNPOD_GRAPHQL_URL", server.URL)

	_, err := GetModels(&GetModelsInput{})
	if err == nil {
		t.Fatal("expected an error for a non-200 graphql response")
	}

	var gqlErr *internalapi.GraphQLError
	if !errors.As(err, &gqlErr) {
		t.Fatalf("expected a *internalapi.GraphQLError, got %#v (%v)", err, err)
	}
	if gqlErr.ErrorCode() != "graphql_error" {
		t.Fatalf("expected code graphql_error, got %q", gqlErr.ErrorCode())
	}
	if gqlErr.HTTPStatus() != http.StatusServiceUnavailable {
		t.Fatalf("expected http status %d preserved, got %d", http.StatusServiceUnavailable, gqlErr.HTTPStatus())
	}
}
