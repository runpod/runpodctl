package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
	internalapi "github.com/runpod/runpodctl/internal/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestSetModelGraphQLTimeoutWithoutInheritedFlag(t *testing.T) {
	viper.Reset()

	cmd := &cobra.Command{Use: "add"}
	stdout, stderr := captureStdStreams(t, func() {
		if err := setModelGraphQLTimeout(cmd); err != nil {
			t.Errorf("setModelGraphQLTimeout: %v", err)
		}
	})

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != modelGraphQLTimeoutValue {
		t.Fatalf("expected graphql timeout %s, got %s", modelGraphQLTimeoutValue, got)
	}
	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr must remain empty, got %q", stderr)
	}
}

func TestSetModelGraphQLTimeoutRespectsExistingConfiguredValue(t *testing.T) {
	viper.Reset()

	existing := 2 * time.Minute
	viper.Set(api.GraphQLTimeoutKey, existing)

	cmd := &cobra.Command{Use: "add"}
	if err := setModelGraphQLTimeout(cmd); err != nil {
		t.Fatalf("setModelGraphQLTimeout: %v", err)
	}

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != existing {
		t.Fatalf("expected graphql timeout to remain %s, got %s", existing, got)
	}
}

func TestSetModelGraphQLTimeoutSetsInheritedFlagWhenUnchanged(t *testing.T) {
	viper.Reset()

	root := &cobra.Command{Use: "runpodctl"}
	root.PersistentFlags().Duration(graphqlTimeoutFlagName, 10*time.Second, "graphql timeout")
	cmd := &cobra.Command{Use: "add"}
	root.AddCommand(cmd)

	if err := setModelGraphQLTimeout(cmd); err != nil {
		t.Fatalf("setModelGraphQLTimeout: %v", err)
	}

	flag := cmd.InheritedFlags().Lookup(graphqlTimeoutFlagName)
	if flag == nil {
		t.Fatal("expected inherited graphql-timeout flag")
	}
	if got := flag.Value.String(); got != modelGraphQLTimeoutValue.String() {
		t.Fatalf("expected inherited flag value %s, got %s", modelGraphQLTimeoutValue, got)
	}
	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != modelGraphQLTimeoutValue {
		t.Fatalf("expected graphql timeout %s, got %s", modelGraphQLTimeoutValue, got)
	}
}

func TestSetModelGraphQLTimeoutSkipsWhenInheritedFlagChanged(t *testing.T) {
	viper.Reset()

	root := &cobra.Command{Use: "runpodctl"}
	root.PersistentFlags().Duration(graphqlTimeoutFlagName, 10*time.Second, "graphql timeout")
	if err := root.PersistentFlags().Set(graphqlTimeoutFlagName, "45s"); err != nil {
		t.Fatalf("failed to set inherited flag: %v", err)
	}

	cmd := &cobra.Command{Use: "add"}
	root.AddCommand(cmd)

	if err := setModelGraphQLTimeout(cmd); err != nil {
		t.Fatalf("setModelGraphQLTimeout: %v", err)
	}

	if got := viper.GetDuration(api.GraphQLTimeoutKey); got != 0 {
		t.Fatalf("expected graphql timeout to remain unset, got %s", got)
	}
}

func TestRunAddModelPathWaitForHashPrintsCompactOutput(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUploadBatch := createModelRepoUploadBatch
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUploadAll := completeModelRepoUploadAll
	oldGetModelsForAdd := getModelsForAdd
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUploadBatch = oldCreateModelRepoUploadBatch
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUploadAll = oldCompleteModelRepoUploadAll
		getModelsForAdd = oldGetModelsForAdd
	})

	modelDir := t.TempDir()
	modelFile := filepath.Join(modelDir, "weights.bin")
	if err := os.WriteFile(modelFile, []byte("model"), 0600); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	addModelOwner = "user-id"
	addModelName = "test-model"
	addModelDirectoryPath = modelDir
	addModelWaitForHash = true

	var addInput *api.AddModelToRepoInput
	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		copy := *input
		addInput = &copy
		return &api.Model{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "huggingface",
		}, nil
	}
	createModelRepoUploadBatch = func(input *api.CreateModelRepoUploadBatchInput) (*api.ModelRepoUploadBatchResult, error) {
		uploads := make([]*api.ModelRepoUpload, len(input.Files))
		for i, file := range input.Files {
			uploads[i] = &api.ModelRepoUpload{SessionID: "session-" + file.FileName, Key: "key-" + file.FileName}
		}
		return &api.ModelRepoUploadBatchResult{
			Success: true,
			Model: &api.Model{
				ID:       "model-id",
				Owner:    "user-id",
				Name:     "test-model",
				Provider: "LOCAL",
			},
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Uploads: uploads,
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		return nil
	}
	completeModelRepoUploadAll = func(sessionIDs []string) ([]*api.CompleteModelRepoUploadResult, error) {
		completions := make([]*api.CompleteModelRepoUploadResult, len(sessionIDs))
		for i, sessionID := range sessionIDs {
			completions[i] = &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}
		}
		return completions, nil
	}
	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "LOCAL",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusPodReady}},
		}}, nil
	}

	cmd := newTestAddModelCommand()
	stdout, _ := captureStdStreams(t, func() {
		if err := runAddModel(cmd, nil); err != nil {
			t.Errorf("runAddModel: %v", err)
		}
	})

	if addInput == nil {
		t.Fatal("expected addModelToRepo to be called")
	}
	if addInput.Provider != "LOCAL" {
		t.Fatalf("expected upload addModelToRepo provider LOCAL, got %q", addInput.Provider)
	}

	var output compactModelAddOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if output.Model.ID != "model-id" {
		t.Fatalf("expected compact model id, got %q", output.Model.ID)
	}
	if output.Model.Name != "test-model" {
		t.Fatalf("expected compact model name, got %q", output.Model.Name)
	}
	if output.Model.Owner != "user-id" {
		t.Fatalf("expected compact model owner, got %q", output.Model.Owner)
	}
	if strings.Contains(stdout, "uploadedFiles") || strings.Contains(stdout, "modelUrl") || strings.Contains(stdout, "modelHash") {
		t.Fatalf("expected compact output, got %s", stdout)
	}
}

func TestRunAddModelPathWaitForHashVerbosePrintsFullOutput(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUploadBatch := createModelRepoUploadBatch
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUploadAll := completeModelRepoUploadAll
	oldGetModelsForAdd := getModelsForAdd
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUploadBatch = oldCreateModelRepoUploadBatch
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUploadAll = oldCompleteModelRepoUploadAll
		getModelsForAdd = oldGetModelsForAdd
	})

	modelDir := t.TempDir()
	modelFile := filepath.Join(modelDir, "weights.bin")
	if err := os.WriteFile(modelFile, []byte("model"), 0600); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	addModelOwner = "user-id"
	addModelName = "test-model"
	addModelDirectoryPath = modelDir
	addModelWaitForHash = true
	addModelVerbose = true

	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		return &api.Model{ID: "model-id", Owner: "user-id", Name: "test-model", Provider: "huggingface"}, nil
	}
	createModelRepoUploadBatch = func(input *api.CreateModelRepoUploadBatchInput) (*api.ModelRepoUploadBatchResult, error) {
		uploads := make([]*api.ModelRepoUpload, len(input.Files))
		for i, file := range input.Files {
			uploads[i] = &api.ModelRepoUpload{SessionID: "session-" + file.FileName, Key: "key-" + file.FileName}
		}
		return &api.ModelRepoUploadBatchResult{
			Success: true,
			Model:   &api.Model{ID: "model-id", Owner: "user-id", Name: "test-model", Provider: "LOCAL"},
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Uploads: uploads,
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		return nil
	}
	completeModelRepoUploadAll = func(sessionIDs []string) ([]*api.CompleteModelRepoUploadResult, error) {
		completions := make([]*api.CompleteModelRepoUploadResult, len(sessionIDs))
		for i, sessionID := range sessionIDs {
			completions[i] = &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}
		}
		return completions, nil
	}
	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "LOCAL",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusPodReady}},
		}}, nil
	}

	cmd := newTestAddModelCommand()
	stdout, _ := captureStdStreams(t, func() {
		if err := runAddModel(cmd, nil); err != nil {
			t.Errorf("runAddModel: %v", err)
		}
	})

	var output modelAddOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if output.Model == nil {
		t.Fatal("expected output model")
	}
	if output.Model.Provider != "LOCAL" {
		t.Fatalf("expected output model provider LOCAL, got %q", output.Model.Provider)
	}
	if output.ModelURL != "https://local/user-id/test-model:hash-123" {
		t.Fatalf("expected local model url, got %q", output.ModelURL)
	}
	if output.ModelHash != "hash-123" {
		t.Fatalf("expected model hash, got %q", output.ModelHash)
	}
	if len(output.UploadedFiles) != 1 {
		t.Fatalf("expected uploaded file details, got %#v", output.UploadedFiles)
	}
}

func TestRunAddModelNonUploadLeavesProviderUnset(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUpload := createModelRepoUpload
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
	})

	addModelOwner = "user-id"
	addModelName = "remote-model"

	var addInput *api.AddModelToRepoInput
	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		copy := *input
		addInput = &copy
		return &api.Model{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "remote-model",
			Provider: "huggingface",
		}, nil
	}
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		t.Fatal("non-upload model add must not create an upload session")
		return nil, nil
	}

	cmd := newTestAddModelCommand()
	stdout, _ := captureStdStreams(t, func() {
		if err := runAddModel(cmd, nil); err != nil {
			t.Errorf("runAddModel: %v", err)
		}
	})

	if addInput == nil {
		t.Fatal("expected addModelToRepo to be called")
	}
	if addInput.Provider != "" {
		t.Fatalf("expected non-upload provider to be unset, got %q", addInput.Provider)
	}

	var output modelAddOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if output.Model == nil || output.Model.Provider != "huggingface" {
		t.Fatalf("expected existing provider in output to be preserved, got %#v", output.Model)
	}
	if output.Upload != nil || len(output.UploadedFiles) != 0 || output.ModelURL != "" {
		t.Fatalf("expected non-upload output only, got %#v", output)
	}
}

func TestRunAddModelHuggingFaceMirrorUsesOnlyAddMutation(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUpload := createModelRepoUpload
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
	})

	addModelOwner = "destination-owner"
	addModelName = "tiny-llm"
	addModelHuggingFaceModel = "arnir0/Tiny-LLM"
	addModelMetadata = map[string]string{"purpose": "test"}

	var addInput *api.AddModelToRepoInput
	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		copy := *input
		addInput = &copy
		return &api.Model{ID: "model-id", Owner: "destination-owner", Name: "tiny-llm", Provider: "LOCAL", Versions: []*api.ModelVersion{{Hash: "version-hash", Status: "PENDING_TRANSFER"}}}, nil
	}
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		t.Fatal("mirror must not create an upload session")
		return nil, nil
	}

	cmd := newTestAddModelCommand()
	var runErr error
	stdout, _ := captureStdStreams(t, func() { runErr = runAddModel(cmd, nil) })
	if runErr != nil {
		t.Fatalf("runAddModel returned error: %v", runErr)
	}

	if addInput == nil {
		t.Fatal("expected addModelToRepo to be called")
	}
	if addInput.HuggingFaceModel != "arnir0/Tiny-LLM" {
		t.Fatalf("expected hugging face model, got %q", addInput.HuggingFaceModel)
	}
	if addInput.Provider != "" {
		t.Fatalf("expected mirror provider to be unset, got %q", addInput.Provider)
	}
	if addInput.Owner != "destination-owner" {
		t.Fatalf("expected destination owner to pass through, got %q", addInput.Owner)
	}
	if got := addInput.Metadata["purpose"]; got != "test" {
		t.Fatalf("expected mirror metadata to pass through, got %#v", addInput.Metadata)
	}

	var output modelAddOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if output.Model == nil || len(output.Model.Versions) != 1 {
		t.Fatalf("expected returned mirror version, got %#v", output.Model)
	}
	if got := output.Model.Versions[0].Hash; got != "version-hash" {
		t.Fatalf("expected mirror version hash, got %q", got)
	}
	if got := output.Model.Versions[0].Status; got != "PENDING_TRANSFER" {
		t.Fatalf("expected mirror version status PENDING_TRANSFER, got %q", got)
	}
}

func TestRunAddModelHuggingFaceMirrorOwnerIsOptional(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUpload := createModelRepoUpload
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
	})

	addModelName = "tiny-llm"
	addModelHuggingFaceModel = "arnir0/Tiny-LLM"

	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		if input.Owner != "" {
			t.Fatalf("expected omitted destination owner, got %q", input.Owner)
		}
		return &api.Model{ID: "model-id", Name: "tiny-llm", Provider: "LOCAL"}, nil
	}
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		t.Fatal("mirror must not create an upload session")
		return nil, nil
	}

	var runErr error
	captureStdStreams(t, func() { runErr = runAddModel(newTestAddModelCommand(), nil) })
	if runErr != nil {
		t.Fatalf("runAddModel returned error: %v", runErr)
	}
}

func TestRunAddModelRejectsMirrorUploadCombinationsBeforeMutation(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{name: "model path", setup: func() { addModelDirectoryPath = "model" }},
		{name: "create upload", setup: func() { addModelCreateUpload = true }},
		{name: "file name", setup: func() { addModelFileName = "weights.bin" }},
		{name: "file size", setup: func() { addModelFileSize = "1" }},
		{name: "part size", setup: func() { addModelPartSize = "1" }},
		{name: "content type", setup: func() { addModelContentType = "application/octet-stream" }},
		{name: "wait for hash", setup: func() { addModelWaitForHash = true }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetAddModelGlobals(t)
			oldAddModelToRepo := addModelToRepo
			oldCreateModelRepoUpload := createModelRepoUpload
			t.Cleanup(func() {
				addModelToRepo = oldAddModelToRepo
				createModelRepoUpload = oldCreateModelRepoUpload
			})

			addModelHuggingFaceModel = "arnir0/Tiny-LLM"
			tt.setup()
			addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
				t.Fatal("invalid mirror flags must be rejected before addModelToRepo")
				return nil, nil
			}
			createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
				t.Fatal("invalid mirror flags must be rejected before createModelRepoUpload")
				return nil, nil
			}

			if err := runAddModel(newTestAddModelCommand(), nil); err == nil {
				t.Fatal("expected incompatible flags error")
			}
		})
	}
}

func TestRunAddModelMetadataWithoutMirrorStillRequestsUpload(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	t.Cleanup(func() { addModelToRepo = oldAddModelToRepo })

	addModelName = "local-model"
	addModelMetadata = map[string]string{"purpose": "test"}
	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		t.Fatal("metadata-only upload must validate upload details before mutation")
		return nil, nil
	}

	err := runAddModel(newTestAddModelCommand(), nil)
	if err == nil || !strings.Contains(err.Error(), "file-name is required") {
		t.Fatalf("expected metadata to retain local upload validation, got %v", err)
	}
}

func TestRunAddModelLocalUploadWithMetadataRemainsUnchanged(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUpload := createModelRepoUpload
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
	})

	addModelName = "local-model"
	addModelFileName = "weights.bin"
	addModelFileSize = "10"
	addModelMetadata = map[string]string{"purpose": "test"}

	var addInput *api.AddModelToRepoInput
	var uploadInput *api.CreateModelRepoUploadInput
	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		copy := *input
		addInput = &copy
		return &api.Model{ID: "model-id", Name: "local-model", Provider: "LOCAL"}, nil
	}
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		copy := *input
		uploadInput = &copy
		return &api.ModelRepoMutationResult{
			Model:  &api.Model{ID: "model-id", Name: "local-model", Provider: "LOCAL"},
			Upload: &api.ModelRepoUpload{SessionID: "session-id"},
		}, nil
	}

	var runErr error
	captureStdStreams(t, func() { runErr = runAddModel(newTestAddModelCommand(), nil) })
	if runErr != nil {
		t.Fatalf("runAddModel returned error: %v", runErr)
	}
	if addInput == nil || addInput.Provider != "LOCAL" || addInput.Metadata["purpose"] != "test" {
		t.Fatalf("expected local add input with provider and metadata, got %#v", addInput)
	}
	if uploadInput == nil || uploadInput.Metadata["purpose"] != "test" {
		t.Fatalf("expected local upload session with metadata, got %#v", uploadInput)
	}
}

func TestUploadModelFilesCreatesOneBatchWhenManifestFits(t *testing.T) {
	oldCreateModelRepoUploadBatch := createModelRepoUploadBatch
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUploadAll := completeModelRepoUploadAll
	t.Cleanup(func() {
		createModelRepoUploadBatch = oldCreateModelRepoUploadBatch
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUploadAll = oldCompleteModelRepoUploadAll
	})

	files := []modelFile{
		{AbsolutePath: "/tmp/a.bin", RelativePath: "a.bin", Size: 1},
		{AbsolutePath: "/tmp/b.bin", RelativePath: "b.bin", Size: 2},
		{AbsolutePath: "/tmp/c.bin", RelativePath: "c.bin", Size: 3},
	}

	var calls []api.CreateModelRepoUploadBatchInput
	createModelRepoUploadBatch = func(input *api.CreateModelRepoUploadBatchInput) (*api.ModelRepoUploadBatchResult, error) {
		calls = append(calls, *input)
		uploads := make([]*api.ModelRepoUpload, len(input.Files))
		for i, file := range input.Files {
			uploads[i] = &api.ModelRepoUpload{
				SessionID: "session-" + file.FileName,
				Key:       "key-" + file.FileName,
			}
		}
		return &api.ModelRepoUploadBatchResult{
			Success: true,
			Version: &api.ModelVersion{
				UUID: "version-uuid",
				Hash: "version-hash",
			},
			Uploads: uploads,
		}, nil
	}
	// uploadModelFiles now uploads a chunk's files concurrently (bounded by
	// modelRepoUploadConcurrency), so the upload mock can be called from multiple
	// goroutines at once: guard the shared slices, and the assertions below only
	// check upload-side ordering as a set, not a fixed sequence. Finalizing is a
	// single completeModelRepoUploadAll call over the whole uploadedFiles list (in
	// file order), so it needs no such guard.
	var mu sync.Mutex
	var events []string
	var uploadedArtifacts []string
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "upload:"+artifactPath)
		uploadedArtifacts = append(uploadedArtifacts, artifactPath)
		return nil
	}
	var completeBatchCalls [][]string
	completeModelRepoUploadAll = func(sessionIDs []string) ([]*api.CompleteModelRepoUploadResult, error) {
		completeBatchCalls = append(completeBatchCalls, append([]string(nil), sessionIDs...))
		events = append(events, "complete-batch:"+strings.Join(sessionIDs, ","))
		completions := make([]*api.CompleteModelRepoUploadResult, len(sessionIDs))
		for i, sessionID := range sessionIDs {
			completions[i] = &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}
		}
		return completions, nil
	}

	uploadedFiles, uploadModel, modelVersionUUID, err := uploadModelFiles(files, &api.CreateModelRepoUploadInput{Name: "test-model"})
	if err != nil {
		t.Fatalf("uploadModelFiles returned error: %v", err)
	}
	if uploadModel != nil {
		t.Fatalf("expected upload model to be nil, got %#v", uploadModel)
	}
	if modelVersionUUID != "version-uuid" {
		t.Fatalf("expected model version uuid %q, got %q", "version-uuid", modelVersionUUID)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 createModelRepoUploadBatch call, got %d", len(calls))
	}
	if calls[0].ModelVersionUUID != "" {
		t.Fatalf("expected the only batch call to omit modelVersionUuid, got %q", calls[0].ModelVersionUUID)
	}
	if len(calls[0].Files) != len(files) {
		t.Fatalf("expected batch call to carry %d files, got %d", len(files), len(calls[0].Files))
	}
	for i, file := range files {
		if calls[0].Files[i].FileName != file.RelativePath {
			t.Fatalf("expected batch file %d name %q, got %q", i, file.RelativePath, calls[0].Files[i].FileName)
		}
	}

	// Chunk uploads run concurrently now, so only the set of uploaded artifacts is
	// guaranteed, not the order the mock observed them in.
	if len(uploadedArtifacts) != len(files) {
		t.Fatalf("expected %d uploaded artifacts, got %d", len(files), len(uploadedArtifacts))
	}
	gotArtifacts := append([]string(nil), uploadedArtifacts...)
	sort.Strings(gotArtifacts)
	wantArtifacts := make([]string, len(files))
	for i, file := range files {
		wantArtifacts[i] = file.AbsolutePath
	}
	sort.Strings(wantArtifacts)
	for i := range wantArtifacts {
		if gotArtifacts[i] != wantArtifacts[i] {
			t.Fatalf("expected uploaded artifacts %v, got %v", wantArtifacts, gotArtifacts)
		}
	}
	// Finalizing is a single completeModelRepoUploadAll call carrying every session id
	// in file order (the list "as it exists" once every chunk has finished uploading).
	expectedCompletedSessions := []string{"session-a.bin", "session-b.bin", "session-c.bin"}
	if len(completeBatchCalls) != 1 {
		t.Fatalf("expected 1 completeModelRepoUploadAll call, got %d", len(completeBatchCalls))
	}
	if len(completeBatchCalls[0]) != len(expectedCompletedSessions) {
		t.Fatalf("expected %d session ids in the batch call, got %d", len(expectedCompletedSessions), len(completeBatchCalls[0]))
	}
	for i, expected := range expectedCompletedSessions {
		if completeBatchCalls[0][i] != expected {
			t.Fatalf("expected batch call session %d to be %q, got %q", i, expected, completeBatchCalls[0][i])
		}
	}
	if len(uploadedFiles) != len(expectedCompletedSessions) {
		t.Fatalf("expected %d uploaded files, got %d", len(expectedCompletedSessions), len(uploadedFiles))
	}
	for i, expected := range expectedCompletedSessions {
		if uploadedFiles[i].SessionID != expected {
			t.Fatalf("expected uploaded file session %d to be %q, got %q", i, expected, uploadedFiles[i].SessionID)
		}
		if uploadedFiles[i].Status != "completed" {
			t.Fatalf("expected uploaded file status %d to be completed, got %q", i, uploadedFiles[i].Status)
		}
	}
	// All 3 uploads happen (concurrently, in any order) before the single finalize
	// call, since completeModelRepoUploadAll only runs once every chunk upload has
	// returned; the 3 upload events themselves are no longer in a fixed order, but the
	// one finalize call is always last and always carries all 3 sessions.
	expectedUploadEvents := []string{"upload:/tmp/a.bin", "upload:/tmp/b.bin", "upload:/tmp/c.bin"}
	expectedCompleteEvent := "complete-batch:session-a.bin,session-b.bin,session-c.bin"
	if len(events) != len(expectedUploadEvents)+1 {
		t.Fatalf("expected %d upload/completion events, got %d", len(expectedUploadEvents)+1, len(events))
	}
	gotUploadEvents := append([]string(nil), events[:len(expectedUploadEvents)]...)
	sort.Strings(gotUploadEvents)
	wantUploadEvents := append([]string(nil), expectedUploadEvents...)
	sort.Strings(wantUploadEvents)
	for i := range wantUploadEvents {
		if gotUploadEvents[i] != wantUploadEvents[i] {
			t.Fatalf("expected upload events %v, got %v", wantUploadEvents, gotUploadEvents)
		}
	}
	if events[len(events)-1] != expectedCompleteEvent {
		t.Fatalf("expected final event %q, got %q", expectedCompleteEvent, events[len(events)-1])
	}
}

func TestUploadModelFilesChunksLargeManifestsAcrossMultipleBatches(t *testing.T) {
	oldCreateModelRepoUploadBatch := createModelRepoUploadBatch
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUploadAll := completeModelRepoUploadAll
	t.Cleanup(func() {
		createModelRepoUploadBatch = oldCreateModelRepoUploadBatch
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUploadAll = oldCompleteModelRepoUploadAll
	})

	fileCount := modelRepoUploadBatchSize + 3
	files := make([]modelFile, fileCount)
	for i := range files {
		name := fmt.Sprintf("file-%04d.bin", i)
		files[i] = modelFile{AbsolutePath: "/tmp/" + name, RelativePath: name, Size: 1}
	}

	var batchSizes []int
	var sawVersionUUIDOnFirstCall bool
	callIndex := 0
	createModelRepoUploadBatch = func(input *api.CreateModelRepoUploadBatchInput) (*api.ModelRepoUploadBatchResult, error) {
		batchSizes = append(batchSizes, len(input.Files))
		if callIndex == 0 && input.ModelVersionUUID != "" {
			sawVersionUUIDOnFirstCall = true
		}
		if callIndex > 0 && input.ModelVersionUUID != "version-uuid" {
			t.Fatalf("expected batch call %d to pin modelVersionUuid, got %q", callIndex, input.ModelVersionUUID)
		}
		callIndex++

		uploads := make([]*api.ModelRepoUpload, len(input.Files))
		for i, file := range input.Files {
			uploads[i] = &api.ModelRepoUpload{SessionID: "session-" + file.FileName, Key: "key-" + file.FileName}
		}
		return &api.ModelRepoUploadBatchResult{
			Success: true,
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Uploads: uploads,
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		return nil
	}
	completeModelRepoUploadAll = func(sessionIDs []string) ([]*api.CompleteModelRepoUploadResult, error) {
		completions := make([]*api.CompleteModelRepoUploadResult, len(sessionIDs))
		for i, sessionID := range sessionIDs {
			completions[i] = &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}
		}
		return completions, nil
	}

	uploadedFiles, _, modelVersionUUID, err := uploadModelFiles(files, &api.CreateModelRepoUploadInput{Name: "test-model"})
	if err != nil {
		t.Fatalf("uploadModelFiles returned error: %v", err)
	}
	if modelVersionUUID != "version-uuid" {
		t.Fatalf("expected model version uuid %q, got %q", "version-uuid", modelVersionUUID)
	}
	if sawVersionUUIDOnFirstCall {
		t.Fatal("expected the first batch call to omit modelVersionUuid")
	}
	if len(uploadedFiles) != fileCount {
		t.Fatalf("expected %d uploaded files, got %d", fileCount, len(uploadedFiles))
	}

	expectedBatchSizes := []int{modelRepoUploadBatchSize, 3}
	if len(batchSizes) != len(expectedBatchSizes) {
		t.Fatalf("expected %d batch calls, got %d (%v)", len(expectedBatchSizes), len(batchSizes), batchSizes)
	}
	for i, expected := range expectedBatchSizes {
		if batchSizes[i] != expected {
			t.Fatalf("expected batch %d to contain %d files, got %d", i, expected, batchSizes[i])
		}
	}
}

// TestUploadModelFilesFinalizesAllSessionsInOneBatchCall proves the finalize step sends
// every uploaded session in a single completeModelRepoUploadAll call -- the list "as it
// exists" once every upload chunk has returned -- rather than one call per file or per
// chunk, even when the manifest spans multiple createModelRepoUploadBatch chunks.
func TestUploadModelFilesFinalizesAllSessionsInOneBatchCall(t *testing.T) {
	oldCreateModelRepoUploadBatch := createModelRepoUploadBatch
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUploadAll := completeModelRepoUploadAll
	t.Cleanup(func() {
		createModelRepoUploadBatch = oldCreateModelRepoUploadBatch
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUploadAll = oldCompleteModelRepoUploadAll
	})

	fileCount := modelRepoUploadBatchSize + 3
	files := make([]modelFile, fileCount)
	for i := range files {
		name := fmt.Sprintf("file-%04d.bin", i)
		files[i] = modelFile{AbsolutePath: "/tmp/" + name, RelativePath: name, Size: 1}
	}

	createModelRepoUploadBatch = func(input *api.CreateModelRepoUploadBatchInput) (*api.ModelRepoUploadBatchResult, error) {
		uploads := make([]*api.ModelRepoUpload, len(input.Files))
		for i, file := range input.Files {
			uploads[i] = &api.ModelRepoUpload{SessionID: "session-" + file.FileName, Key: "key-" + file.FileName}
		}
		return &api.ModelRepoUploadBatchResult{
			Success: true,
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Uploads: uploads,
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		return nil
	}

	var completeBatchCalls [][]string
	completeModelRepoUploadAll = func(sessionIDs []string) ([]*api.CompleteModelRepoUploadResult, error) {
		completeBatchCalls = append(completeBatchCalls, append([]string(nil), sessionIDs...))
		completions := make([]*api.CompleteModelRepoUploadResult, len(sessionIDs))
		for i, sessionID := range sessionIDs {
			completions[i] = &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}
		}
		return completions, nil
	}

	uploadedFiles, _, _, err := uploadModelFiles(files, &api.CreateModelRepoUploadInput{Name: "test-model"})
	if err != nil {
		t.Fatalf("uploadModelFiles returned error: %v", err)
	}

	// One call, spanning both upload-batch chunks' sessions, in file order.
	if len(completeBatchCalls) != 1 {
		t.Fatalf("expected 1 completeModelRepoUploadAll call, got %d", len(completeBatchCalls))
	}
	if len(completeBatchCalls[0]) != fileCount {
		t.Fatalf("expected %d session ids in the batch call, got %d", fileCount, len(completeBatchCalls[0]))
	}
	for i, file := range files {
		expected := "session-" + file.RelativePath
		if completeBatchCalls[0][i] != expected {
			t.Fatalf("expected batch call session %d to be %q, got %q", i, expected, completeBatchCalls[0][i])
		}
	}

	if len(uploadedFiles) != fileCount {
		t.Fatalf("expected %d uploaded files, got %d", fileCount, len(uploadedFiles))
	}
	for i, file := range files {
		if uploadedFiles[i].RelativePath != file.RelativePath {
			t.Fatalf("expected uploaded file %d to be %q, got %q", i, file.RelativePath, uploadedFiles[i].RelativePath)
		}
		if uploadedFiles[i].Status != "completed" {
			t.Fatalf("expected uploaded file %d status to be completed, got %q", i, uploadedFiles[i].Status)
		}
	}
}

func TestUploadModelFileChunkUploadsConcurrentlyWithinBound(t *testing.T) {
	oldCompleteModelUploadFile := completeModelUploadFile
	t.Cleanup(func() { completeModelUploadFile = oldCompleteModelUploadFile })

	const fileCount = modelRepoUploadConcurrency * 3
	chunk := make([]modelFile, fileCount)
	uploads := make([]*api.ModelRepoUpload, fileCount)
	for i := range chunk {
		name := fmt.Sprintf("file-%02d.bin", i)
		chunk[i] = modelFile{AbsolutePath: "/tmp/" + name, RelativePath: name, Size: 1}
		uploads[i] = &api.ModelRepoUpload{SessionID: "session-" + name, Key: "key-" + name}
	}

	var inFlight int32
	var maxInFlight int32
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		current := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		for {
			observed := atomic.LoadInt32(&maxInFlight)
			if current <= observed || atomic.CompareAndSwapInt32(&maxInFlight, observed, current) {
				break
			}
		}
		// Hold the "upload" open briefly so concurrent calls actually overlap instead
		// of racing to completion faster than the scheduler interleaves them.
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	results, err := uploadModelFileChunk(chunk, uploads, nil)
	if err != nil {
		t.Fatalf("uploadModelFileChunk returned error: %v", err)
	}
	if len(results) != fileCount {
		t.Fatalf("expected %d results, got %d", fileCount, len(results))
	}
	// Results are indexed by chunk position, so they must come back in the same order
	// as the input regardless of which goroutine finished first.
	for i, file := range chunk {
		if results[i].RelativePath != file.RelativePath {
			t.Fatalf("expected result %d to be %q, got %q", i, file.RelativePath, results[i].RelativePath)
		}
	}

	got := atomic.LoadInt32(&maxInFlight)
	if got <= 1 {
		t.Fatalf("expected uploads to run concurrently (max in flight > 1), got %d", got)
	}
	if got > modelRepoUploadConcurrency {
		t.Fatalf("expected at most %d concurrent uploads, observed %d", modelRepoUploadConcurrency, got)
	}
}

func TestUploadModelFileChunkReturnsFirstErrorInChunkOrder(t *testing.T) {
	oldCompleteModelUploadFile := completeModelUploadFile
	t.Cleanup(func() { completeModelUploadFile = oldCompleteModelUploadFile })

	chunk := []modelFile{
		{AbsolutePath: "/tmp/a.bin", RelativePath: "a.bin", Size: 1},
		{AbsolutePath: "/tmp/b.bin", RelativePath: "b.bin", Size: 1},
		{AbsolutePath: "/tmp/c.bin", RelativePath: "c.bin", Size: 1},
	}
	uploads := []*api.ModelRepoUpload{
		{SessionID: "session-a", Key: "key-a"},
		{SessionID: "session-b", Key: "key-b"},
		{SessionID: "session-c", Key: "key-c"},
	}

	// b.bin (chunk index 1) fails, but resolves faster than a.bin and c.bin, which
	// succeed. The chunk-order-index-0 file (a.bin) never errors, so the deterministic
	// first-error-by-index result should still be b.bin's error, not whichever file's
	// goroutine happened to finish (or fail) first.
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		switch artifactPath {
		case "/tmp/a.bin":
			time.Sleep(15 * time.Millisecond)
			return nil
		case "/tmp/b.bin":
			return fmt.Errorf("boom")
		case "/tmp/c.bin":
			time.Sleep(5 * time.Millisecond)
			return nil
		default:
			t.Fatalf("unexpected artifact %q", artifactPath)
			return nil
		}
	}

	_, err := uploadModelFileChunk(chunk, uploads, nil)
	if err == nil {
		t.Fatal("expected uploadModelFileChunk to return an error")
	}
	if !strings.Contains(err.Error(), "b.bin") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error to reference b.bin's failure, got %v", err)
	}
}

type recordingModelUploadProgress struct {
	bytes    int64
	finished bool
	cleared  bool
}

func (p *recordingModelUploadProgress) Add64(n int64) error {
	p.bytes += n
	return nil
}

func (p *recordingModelUploadProgress) Finish() error {
	p.finished = true
	return nil
}

func (p *recordingModelUploadProgress) Clear() error {
	p.cleared = true
	return nil
}

func TestProgressReaderTracksBytes(t *testing.T) {
	progress := &recordingModelUploadProgress{}
	reader := progressReader{
		reader:   strings.NewReader("abcdef"),
		progress: progress,
	}

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("copy progress reader: %v", err)
	}
	if progress.bytes != 6 {
		t.Fatalf("expected 6 progress bytes, got %d", progress.bytes)
	}
}

func TestPrintCompletedModelUploadSizeWritesToStderr(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printCompletedModelUploadSize(123)
	})

	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	if stderr != "model size: 123 bytes\n" {
		t.Fatalf("expected model size on stderr, got %q", stderr)
	}
}

func TestWaitForUploadedModelHashPollsUntilHash(t *testing.T) {
	oldGetModelsForAdd := getModelsForAdd
	oldSleepModelHashPoll := sleepModelHashPoll
	t.Cleanup(func() {
		getModelsForAdd = oldGetModelsForAdd
		sleepModelHashPoll = oldSleepModelHashPoll
	})

	var calls int
	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		calls++
		if input == nil || input.Name != "test-model" {
			t.Fatalf("expected model lookup by name test-model, got %#v", input)
		}
		if calls == 1 {
			// the api assigns a placeholder hash the moment the version exists, so
			// a nonempty hash on a NEEDS_HASH version must not end the wait.
			return []*api.Model{
				{
					ID:       "model-id",
					Owner:    "user-id",
					Name:     "test-model",
					Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusNeedsHash}, {UUID: "old-version", Hash: "old-hash"}},
				},
			}, nil
		}
		return []*api.Model{
			{
				ID:       "model-id",
				Owner:    "user-id",
				Name:     "test-model",
				Versions: []*api.ModelVersion{{UUID: "old-version", Hash: "old-hash"}, {UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusPodReady}},
			},
		}, nil
	}

	var sleeps []time.Duration
	sleepModelHashPoll = func(ctx context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	}

	var ready *modelReadyOutput
	stdout, stderr := captureStdStreams(t, func() {
		var err error
		ready, err = waitForUploadedModelHash(context.Background(), "", "test-model", &api.Model{ID: "model-id"}, "version-uuid", 123*time.Millisecond)
		if err != nil {
			t.Fatalf("waitForUploadedModelHash returned error: %v", err)
		}
	})

	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	if stderr != "waiting for model to be deployable.\n" {
		t.Fatalf("expected wait progress on stderr, got %q", stderr)
	}
	if calls != 2 {
		t.Fatalf("expected 2 model lookup calls, got %d", calls)
	}
	if len(sleeps) != 1 || sleeps[0] != 123*time.Millisecond {
		t.Fatalf("expected one 123ms sleep, got %#v", sleeps)
	}
	if ready == nil {
		t.Fatal("expected ready output")
	}
	if ready.Owner != "user-id" {
		t.Fatalf("expected owner user-id, got %q", ready.Owner)
	}
	if ready.Name != "test-model" {
		t.Fatalf("expected name test-model, got %q", ready.Name)
	}
	if ready.ModelHash != "hash-123" {
		t.Fatalf("expected hash hash-123, got %q", ready.ModelHash)
	}
	if ready.ModelURL != "https://local/user-id/test-model:hash-123" {
		t.Fatalf("expected model url, got %q", ready.ModelURL)
	}
}

func TestWaitForUploadedModelHashTimesOut(t *testing.T) {
	oldGetModelsForAdd := getModelsForAdd
	oldSleepModelHashPoll := sleepModelHashPoll
	t.Cleanup(func() {
		getModelsForAdd = oldGetModelsForAdd
		sleepModelHashPoll = oldSleepModelHashPoll
	})

	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusNeedsHash}},
		}}, nil
	}
	sleepModelHashPoll = waitModelHashPoll

	// long enough for one poll to observe the status, short enough that the
	// hour-long poll interval below is what the deadline interrupts.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := waitForUploadedModelHash(ctx, "user-id", "test-model", &api.Model{ID: "model-id"}, "version-uuid", time.Hour)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out waiting for the model version to become deployable") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	// which of the two "not deployable yet" cases this was decides the operator's
	// next move, and the status is the only thing that separates them.
	if !strings.Contains(err.Error(), "last version status: needs_hash") {
		t.Errorf("timeout error should name the last version status seen, got %v", err)
	}
	if !strings.Contains(err.Error(), "runpodctl model list --name test-model") {
		t.Errorf("timeout error should name the command to poll with, got %v", err)
	}
	// the upload already succeeded at this point, so the message must tell an
	// agent not to re-upload, and the sink must NOT tag it network_error (which
	// means "transient, retry").
	if !strings.Contains(err.Error(), "do not re-upload") {
		t.Errorf("timeout error should say the model exists, got %v", err)
	}
	// "the cli stopped waiting" is one condition, so it must report one code
	// wherever it happens — an agent branching on code == "timeout" to mean
	// "still running, do not retry" has to get the same answer here as it does
	// from serverless run --wait.
	var timeoutErr *internalapi.TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected an *internalapi.TimeoutError, got %T: %v", err, err)
	}
	if timeoutErr.ErrorCode() != "timeout" {
		t.Errorf("code = %q, want timeout", timeoutErr.ErrorCode())
	}
}

func TestWaitForUploadedModelHashRequiresModelVersionUUID(t *testing.T) {
	oldGetModelsForAdd := getModelsForAdd
	t.Cleanup(func() {
		getModelsForAdd = oldGetModelsForAdd
	})

	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		t.Fatal("waitForUploadedModelHash must not poll without a model version uuid")
		return nil, nil
	}

	_, err := waitForUploadedModelHash(context.Background(), "user-id", "test-model", &api.Model{ID: "model-id"}, " ", time.Millisecond)
	if err == nil {
		t.Fatal("expected missing model version uuid error")
	}
	if !strings.Contains(err.Error(), "model version uuid is required to wait for hashing") {
		t.Fatalf("expected missing model version uuid error, got %v", err)
	}
}

func TestUploadedModelVersionStateRequiresMatchingVersionUUID(t *testing.T) {
	model := &api.Model{Versions: []*api.ModelVersion{
		{UUID: "old-version", Hash: "old-hash", Status: api.ModelVersionStatusPodReady},
		{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusNeedsHash},
		{UUID: "newer-version", Hash: "newer-hash", Status: api.ModelVersionStatusPodReady},
	}}

	if got := uploadedModelVersionState(model, ""); got.hash != "" {
		t.Fatalf("expected no fallback hash without model version uuid, got %q", got.hash)
	}

	if got := uploadedModelVersionState(model, "version-uuid"); got.hash != "" {
		t.Fatalf("expected no hash for pending uploaded version, got %q", got.hash)
	}

	model.Versions[1].Hash = "hash-123"
	model.Versions[1].Status = api.ModelVersionStatusPodReady
	if got := uploadedModelVersionState(model, "version-uuid"); got.hash != "hash-123" {
		t.Fatalf("expected uploaded version hash, got %q", got.hash)
	}
}

func TestUploadedModelVersionStateReadiness(t *testing.T) {
	tests := []struct {
		name         string
		version      *api.ModelVersion
		wantHash     string
		wantTerminal bool
	}{
		{
			name:    "placeholder hash while needs_hash is not deployable",
			version: &api.ModelVersion{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusNeedsHash},
		},
		{
			name:    "real hash while needs_hash is not deployable",
			version: &api.ModelVersion{UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusNeedsHash},
		},
		{
			name:    "placeholder hash on a deployable status is not deployable",
			version: &api.ModelVersion{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusPodReady},
		},
		{
			name:    "validating is not deployable, and not terminal either",
			version: &api.ModelVersion{UUID: "version-uuid", Hash: "hash-123", Status: "VALIDATING"},
		},
		{
			name:     "pod_ready with a real hash is deployable",
			version:  &api.ModelVersion{UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusPodReady},
			wantHash: "hash-123",
		},
		{
			name:     "ready with a real hash is deployable",
			version:  &api.ModelVersion{UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusReady},
			wantHash: "hash-123",
		},
		{
			name:         "failed is terminal",
			version:      &api.ModelVersion{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusFailed},
			wantTerminal: true,
		},
		{
			name:         "deprecated without a canonical pointer is terminal",
			version:      &api.ModelVersion{UUID: "version-uuid", Hash: "hash-123", Status: api.ModelVersionStatusDeprecated},
			wantTerminal: true,
		},
		{
			// the api dedupes an upload whose content already exists: this uuid is
			// deprecated and keeps its placeholder hash forever, and the canonical
			// version it names is the deployable result. Failing here would fail a
			// successful upload.
			name: "deprecated with a canonical pointer resolves to the canonical hash",
			version: &api.ModelVersion{
				UUID:   "version-uuid",
				Hash:   "ph-0123456789abcdef",
				Status: api.ModelVersionStatusDeprecated,
				Metadata: map[string]interface{}{
					"dedupedToCanonicalVersion": map[string]interface{}{
						"uuid": "canonical-uuid",
						"hash": "canonical-hash",
					},
				},
			},
			wantHash: "canonical-hash",
		},
		{
			name: "canonical pointer with no usable hash is still terminal",
			version: &api.ModelVersion{
				UUID:   "version-uuid",
				Hash:   "ph-0123456789abcdef",
				Status: api.ModelVersionStatusDeprecated,
				Metadata: map[string]interface{}{
					"dedupedToCanonicalVersion": map[string]interface{}{"uuid": "canonical-uuid"},
				},
			},
			wantTerminal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uploadedModelVersionState(&api.Model{Versions: []*api.ModelVersion{tt.version}}, "version-uuid")
			if got.hash != tt.wantHash {
				t.Errorf("hash = %q, want %q", got.hash, tt.wantHash)
			}
			if got.terminal != tt.wantTerminal {
				t.Errorf("terminal = %v, want %v", got.terminal, tt.wantTerminal)
			}
			if got.status != strings.ToUpper(tt.version.Status) {
				t.Errorf("status = %q, want %q", got.status, strings.ToUpper(tt.version.Status))
			}
		})
	}
}

func TestWaitForUploadedModelHashStopsOnTerminalVersionStatus(t *testing.T) {
	oldGetModelsForAdd := getModelsForAdd
	oldSleepModelHashPoll := sleepModelHashPoll
	t.Cleanup(func() {
		getModelsForAdd = oldGetModelsForAdd
		sleepModelHashPoll = oldSleepModelHashPoll
	})

	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusFailed}},
		}}, nil
	}
	sleepModelHashPoll = func(ctx context.Context, duration time.Duration) error {
		t.Fatal("a version that failed server-side must not be polled again")
		return nil
	}

	var err error
	stdout, _ := captureStdStreams(t, func() {
		_, err = waitForUploadedModelHash(context.Background(), "user-id", "test-model", &api.Model{ID: "model-id"}, "version-uuid", time.Hour)
	})

	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	if err == nil {
		t.Fatal("expected an error for a failed model version")
	}
	if !strings.Contains(err.Error(), "will never become deployable") {
		t.Errorf("error should say waiting is pointless, got %v", err)
	}
	// not `timeout`: that code means the work is still running server-side and
	// tells an agent to poll rather than re-upload. Here re-uploading is the fix.
	var apiErr *internalapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *internalapi.APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorCode() != "conflict" {
		t.Errorf("code = %q, want conflict", apiErr.ErrorCode())
	}
}

func TestWaitForUploadedModelHashResolvesDedupedVersion(t *testing.T) {
	oldGetModelsForAdd := getModelsForAdd
	t.Cleanup(func() { getModelsForAdd = oldGetModelsForAdd })

	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:    "model-id",
			Owner: "user-id",
			Name:  "test-model",
			Versions: []*api.ModelVersion{{
				UUID:   "version-uuid",
				Hash:   "ph-0123456789abcdef",
				Status: api.ModelVersionStatusDeprecated,
				Metadata: map[string]interface{}{
					"dedupedToCanonicalVersion": map[string]interface{}{
						"uuid": "canonical-uuid",
						"hash": "canonical-hash",
					},
				},
			}},
		}}, nil
	}

	var ready *modelReadyOutput
	captureStdStreams(t, func() {
		var err error
		ready, err = waitForUploadedModelHash(context.Background(), "user-id", "test-model", &api.Model{ID: "model-id"}, "version-uuid", time.Hour)
		if err != nil {
			t.Fatalf("waitForUploadedModelHash returned error: %v", err)
		}
	})

	if ready.ModelHash != "canonical-hash" {
		t.Fatalf("hash = %q, want the canonical version hash", ready.ModelHash)
	}
	if !strings.HasSuffix(ready.ModelURL, ":canonical-hash") {
		t.Fatalf("model url must name the canonical hash, got %q", ready.ModelURL)
	}
}

func TestPrintModelReadyURLWritesToStderr(t *testing.T) {
	stdout, stderr := captureStdStreams(t, func() {
		printModelReadyURL(`https://local/user/model:$hash"quoted`)
	})

	if stdout != "" {
		t.Fatalf("stdout must remain empty, got %q", stdout)
	}
	want := "model is ready to deploy, your model url is: \"https://local/user/model:\\$hash\\\"quoted\"\n"
	if stderr != want {
		t.Fatalf("expected ready message on stderr, got %q", stderr)
	}
}

func TestCompleteModelUploadWithProgressTracksMultipartBytes(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(artifactPath, []byte("abcdef"), 0600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var uploadedBytes int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read upload body: %v", err)
			}
			uploadedBytes += int64(len(body))
			w.Header().Set("ETag", `"etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	progress := &recordingModelUploadProgress{}
	err := completeModelUploadWithProgress(&api.ModelRepoUpload{
		PartSizeBytes: 3,
		CompleteURL:   server.URL,
		Parts: []*api.ModelRepoUploadPart{
			{PartNumber: 2, URL: server.URL},
			{PartNumber: 1, URL: server.URL},
		},
	}, artifactPath, progress)
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if uploadedBytes != 6 {
		t.Fatalf("expected server to receive 6 bytes, got %d", uploadedBytes)
	}
	if progress.bytes != 6 {
		t.Fatalf("expected progress to receive 6 bytes, got %d", progress.bytes)
	}
}

func TestCompleteModelUploadAllowsZeroByteFileWithNoParts(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "empty.bin")
	if err := os.WriteFile(artifactPath, nil, 0600); err != nil {
		t.Fatalf("write empty artifact: %v", err)
	}

	err := completeModelUpload(&api.ModelRepoUpload{}, artifactPath)
	if err != nil {
		t.Fatalf("expected zero-byte upload with no parts to succeed, got %v", err)
	}
}

func newTestAddModelCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "add"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

func resetAddModelGlobals(t *testing.T) {
	t.Helper()

	oldOwner := addModelOwner
	oldName := addModelName
	oldHuggingFaceModel := addModelHuggingFaceModel
	oldCredentialReference := addModelCredentialReference
	oldCredentialType := addModelCredentialType
	oldStatus := addModelStatus
	oldCreateUpload := addModelCreateUpload
	oldFileName := addModelFileName
	oldFileSize := addModelFileSize
	oldPartSize := addModelPartSize
	oldContentType := addModelContentType
	oldDirectoryPath := addModelDirectoryPath
	oldMetadata := addModelMetadata
	oldWaitForHash := addModelWaitForHash
	oldHashTimeout := addModelHashTimeout
	oldVerbose := addModelVerbose
	oldDeleteAfterUpload := addModelDeleteAfterUpload
	t.Cleanup(func() {
		addModelOwner = oldOwner
		addModelName = oldName
		addModelHuggingFaceModel = oldHuggingFaceModel
		addModelCredentialReference = oldCredentialReference
		addModelCredentialType = oldCredentialType
		addModelStatus = oldStatus
		addModelCreateUpload = oldCreateUpload
		addModelFileName = oldFileName
		addModelFileSize = oldFileSize
		addModelPartSize = oldPartSize
		addModelContentType = oldContentType
		addModelDirectoryPath = oldDirectoryPath
		addModelMetadata = oldMetadata
		addModelWaitForHash = oldWaitForHash
		addModelHashTimeout = oldHashTimeout
		addModelVerbose = oldVerbose
		addModelDeleteAfterUpload = oldDeleteAfterUpload
	})

	addModelOwner = ""
	addModelName = ""
	addModelHuggingFaceModel = ""
	addModelCredentialReference = ""
	addModelCredentialType = ""
	addModelStatus = ""
	addModelCreateUpload = false
	addModelFileName = ""
	addModelFileSize = ""
	addModelPartSize = ""
	addModelContentType = ""
	addModelDirectoryPath = ""
	addModelMetadata = nil
	addModelWaitForHash = false
	addModelHashTimeout = modelHashWaitTimeout
	addModelVerbose = false
	addModelDeleteAfterUpload = false
}
