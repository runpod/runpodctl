package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runpod/runpodctl/api"
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
	oldCreateModelRepoUpload := createModelRepoUpload
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUpload := completeModelRepoUpload
	oldGetModelsForAdd := getModelsForAdd
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUpload = oldCompleteModelRepoUpload
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
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		return &api.ModelRepoMutationResult{
			Success: true,
			Model: &api.Model{
				ID:       "model-id",
				Owner:    "user-id",
				Name:     "test-model",
				Provider: "LOCAL",
			},
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Upload: &api.ModelRepoUpload{
				SessionID: "session-id",
				Key:       "key",
			},
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		return nil
	}
	completeModelRepoUpload = func(sessionID string) (*api.CompleteModelRepoUploadResult, error) {
		return &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}, nil
	}
	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "LOCAL",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "hash-123"}},
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
	oldCreateModelRepoUpload := createModelRepoUpload
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUpload := completeModelRepoUpload
	oldGetModelsForAdd := getModelsForAdd
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUpload = oldCompleteModelRepoUpload
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
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		return &api.ModelRepoMutationResult{
			Success: true,
			Model:   &api.Model{ID: "model-id", Owner: "user-id", Name: "test-model", Provider: "LOCAL"},
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Upload:  &api.ModelRepoUpload{SessionID: "session-id", Key: "key"},
		}, nil
	}
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		return nil
	}
	completeModelRepoUpload = func(sessionID string) (*api.CompleteModelRepoUploadResult, error) {
		return &api.CompleteModelRepoUploadResult{SessionID: sessionID, Status: "completed"}, nil
	}
	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "LOCAL",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "hash-123"}},
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

func TestUploadModelFilesUsesModelVersionUUIDAfterFirstFile(t *testing.T) {
	oldCreateModelRepoUpload := createModelRepoUpload
	oldCompleteModelUploadFile := completeModelUploadFile
	oldCompleteModelRepoUpload := completeModelRepoUpload
	t.Cleanup(func() {
		createModelRepoUpload = oldCreateModelRepoUpload
		completeModelUploadFile = oldCompleteModelUploadFile
		completeModelRepoUpload = oldCompleteModelRepoUpload
	})

	files := []modelFile{
		{AbsolutePath: "/tmp/a.bin", RelativePath: "a.bin", Size: 1},
		{AbsolutePath: "/tmp/b.bin", RelativePath: "b.bin", Size: 2},
		{AbsolutePath: "/tmp/c.bin", RelativePath: "c.bin", Size: 3},
	}

	var calls []api.CreateModelRepoUploadInput
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		calls = append(calls, *input)
		return &api.ModelRepoMutationResult{
			Success: true,
			Version: &api.ModelVersion{
				UUID: "version-uuid",
				Hash: "version-hash",
			},
			Upload: &api.ModelRepoUpload{
				SessionID: "session-" + input.FileName,
				Key:       "key-" + input.FileName,
			},
		}, nil
	}
	var events []string
	var uploadedArtifacts []string
	completeModelUploadFile = func(upload *api.ModelRepoUpload, artifactPath string, progress modelUploadProgress) error {
		events = append(events, "upload:"+artifactPath)
		uploadedArtifacts = append(uploadedArtifacts, artifactPath)
		return nil
	}
	var completedSessions []string
	completeModelRepoUpload = func(sessionID string) (*api.CompleteModelRepoUploadResult, error) {
		events = append(events, "complete:"+sessionID)
		completedSessions = append(completedSessions, sessionID)
		return &api.CompleteModelRepoUploadResult{
			SessionID: sessionID,
			Status:    "completed",
		}, nil
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

	if len(calls) != len(files) {
		t.Fatalf("expected %d createModelRepoUpload calls, got %d", len(files), len(calls))
	}
	if calls[0].ModelVersionUUID != "" {
		t.Fatalf("expected first upload call to omit modelVersionUuid, got %q", calls[0].ModelVersionUUID)
	}
	for i := 1; i < len(calls); i++ {
		if calls[i].ModelVersionUUID != "version-uuid" {
			t.Fatalf("expected call %d to use modelVersionUuid %q, got %q", i, "version-uuid", calls[i].ModelVersionUUID)
		}
	}
	if len(uploadedArtifacts) != len(files) {
		t.Fatalf("expected %d uploaded artifacts, got %d", len(files), len(uploadedArtifacts))
	}
	for i, file := range files {
		if uploadedArtifacts[i] != file.AbsolutePath {
			t.Fatalf("expected uploaded artifact %d to be %q, got %q", i, file.AbsolutePath, uploadedArtifacts[i])
		}
	}
	expectedCompletedSessions := []string{"session-a.bin", "session-b.bin", "session-c.bin"}
	if len(completedSessions) != len(expectedCompletedSessions) {
		t.Fatalf("expected %d completed sessions, got %d", len(expectedCompletedSessions), len(completedSessions))
	}
	for i, expected := range expectedCompletedSessions {
		if completedSessions[i] != expected {
			t.Fatalf("expected completed session %d to be %q, got %q", i, expected, completedSessions[i])
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
	expectedEvents := []string{
		"upload:/tmp/a.bin",
		"upload:/tmp/b.bin",
		"upload:/tmp/c.bin",
		"complete:session-a.bin",
		"complete:session-b.bin",
		"complete:session-c.bin",
	}
	if len(events) != len(expectedEvents) {
		t.Fatalf("expected %d upload/completion events, got %d", len(expectedEvents), len(events))
	}
	for i, expected := range expectedEvents {
		if events[i] != expected {
			t.Fatalf("expected event %d to be %q, got %q", i, expected, events[i])
		}
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
			return []*api.Model{
				{
					ID:       "model-id",
					Owner:    "user-id",
					Name:     "test-model",
					Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: ""}, {UUID: "old-version", Hash: "old-hash"}},
				},
			}, nil
		}
		return []*api.Model{
			{
				ID:       "model-id",
				Owner:    "user-id",
				Name:     "test-model",
				Versions: []*api.ModelVersion{{UUID: "old-version", Hash: "old-hash"}, {UUID: "version-uuid", Hash: "hash-123"}},
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
	if stderr != "waiting for model to be hashed.\n" {
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
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: ""}},
		}}, nil
	}
	sleepModelHashPoll = waitModelHashPoll

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	_, err := waitForUploadedModelHash(ctx, "user-id", "test-model", &api.Model{ID: "model-id"}, "version-uuid", time.Hour)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out waiting for the model hash") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	// the upload already succeeded at this point, so the message must tell an
	// agent not to re-upload, and the sink must NOT tag it network_error (which
	// means "transient, retry").
	if !strings.Contains(err.Error(), "do not re-upload") {
		t.Errorf("timeout error should say the model exists, got %v", err)
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

func TestUploadedModelVersionHashRequiresMatchingVersionUUID(t *testing.T) {
	model := &api.Model{Versions: []*api.ModelVersion{
		{UUID: "old-version", Hash: "old-hash"},
		{UUID: "version-uuid", Hash: ""},
		{UUID: "newer-version", Hash: "newer-hash"},
	}}

	if got := uploadedModelVersionHash(model, ""); got != "" {
		t.Fatalf("expected no fallback hash without model version uuid, got %q", got)
	}

	if got := uploadedModelVersionHash(model, "version-uuid"); got != "" {
		t.Fatalf("expected no hash for pending uploaded version, got %q", got)
	}

	model.Versions[1].Hash = "hash-123"
	if got := uploadedModelVersionHash(model, "version-uuid"); got != "hash-123" {
		t.Fatalf("expected uploaded version hash, got %q", got)
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
}
