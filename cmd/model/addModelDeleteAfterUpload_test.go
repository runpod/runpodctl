package model

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/api"
)

// STO-446: runpodctl model add --delete-my-model-files-after-upload deletes
// only the local files that were part of THIS run's upload, and only after
// the model version hash is confirmed server-side. It must never delete
// anything before that confirmation, and must fail loudly (non-zero exit,
// no silent partial success) if confirmation or deletion does not fully
// succeed.

func TestValidateAddModelFlagsRequiresModelPathForDeleteAfterUpload(t *testing.T) {
	resetAddModelGlobals(t)

	addModelDeleteAfterUpload = true
	addModelWaitForHash = true // satisfied, so only the model-path gap should trip

	err := validateAddModelFlags()
	if err == nil || !strings.Contains(err.Error(), "--delete-my-model-files-after-upload requires --model-path") {
		t.Fatalf("expected model-path requirement error, got %v", err)
	}
}

func TestValidateAddModelFlagsRequiresWaitForHashForDeleteAfterUpload(t *testing.T) {
	resetAddModelGlobals(t)

	addModelDeleteAfterUpload = true
	addModelDirectoryPath = "/tmp/some-model-dir" // satisfied, so only the wait-for-hash gap should trip

	err := validateAddModelFlags()
	if err == nil || !strings.Contains(err.Error(), "--delete-my-model-files-after-upload requires --wait-for-hash") {
		t.Fatalf("expected wait-for-hash requirement error, got %v", err)
	}
}

func TestValidateAddModelFlagsAllowsDeleteAfterUploadWithBothRequirements(t *testing.T) {
	resetAddModelGlobals(t)

	addModelDeleteAfterUpload = true
	addModelDirectoryPath = "/tmp/some-model-dir"
	addModelWaitForHash = true

	if err := validateAddModelFlags(); err != nil {
		t.Fatalf("expected no error once both requirements are satisfied, got %v", err)
	}
}

// stubAddModelUploadSeams wires every seam runAddModel needs for a
// model-path + wait-for-hash upload to succeed, returning the confirmed
// hash immediately (no polling). Tests override individual seams after
// calling this to exercise failure paths.
func stubAddModelUploadSeams(t *testing.T) {
	t.Helper()

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

	addModelToRepo = func(input *api.AddModelToRepoInput) (*api.Model, error) {
		return &api.Model{ID: "model-id", Owner: "user-id", Name: "test-model", Provider: "huggingface"}, nil
	}
	createModelRepoUpload = func(input *api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		return &api.ModelRepoMutationResult{
			Success: true,
			Model:   &api.Model{ID: "model-id", Owner: "user-id", Name: "test-model", Provider: "LOCAL"},
			Version: &api.ModelVersion{UUID: "version-uuid"},
			Upload:  &api.ModelRepoUpload{SessionID: "session-" + input.FileName, Key: "key-" + input.FileName},
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
}

func TestRunAddModelDeleteAfterUploadRemovesOnlyVerifiedFiles(t *testing.T) {
	resetAddModelGlobals(t)
	stubAddModelUploadSeams(t)

	modelDir := t.TempDir()
	weightsPath := filepath.Join(modelDir, "weights.bin")
	configPath := filepath.Join(modelDir, "config.json")
	if err := os.WriteFile(weightsPath, []byte("weights"), 0600); err != nil {
		t.Fatalf("write weights file: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("config"), 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	addModelOwner = "user-id"
	addModelName = "test-model"
	addModelDirectoryPath = modelDir
	addModelWaitForHash = true
	addModelDeleteAfterUpload = true
	addModelVerbose = true

	cmd := newTestAddModelCommand()
	stdout, stderr := captureStdStreams(t, func() {
		if err := runAddModel(cmd, nil); err != nil {
			t.Fatalf("runAddModel: %v", err)
		}
	})

	if _, err := os.Stat(weightsPath); !os.IsNotExist(err) {
		t.Fatalf("expected weights.bin to be deleted after verified upload, stat err: %v", err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected config.json to be deleted after verified upload, stat err: %v", err)
	}

	if !strings.Contains(stderr, "deleted 2 verified model file(s)") {
		t.Fatalf("expected deletion confirmation on stderr, got %q", stderr)
	}

	var output modelAddOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if output.DeletedModelFiles != 2 {
		t.Fatalf("expected 2 deleted files reported, got %d", output.DeletedModelFiles)
	}
	if output.DeletedModelFilesBytes != int64(len("weights")+len("config")) {
		t.Fatalf("expected deleted bytes to match file sizes, got %d", output.DeletedModelFilesBytes)
	}
}

func TestRunAddModelDeleteAfterUploadLeavesFilesWhenHashConfirmationFails(t *testing.T) {
	resetAddModelGlobals(t)
	stubAddModelUploadSeams(t)

	// leave sleepModelHashPoll as the real waitModelHashPoll: with
	// addModelHashTimeout set to 1ns below, the context is already expired by
	// the time the poll loop calls it, so its ctx.Done() case fires
	// immediately and the loop reports a timeout on its next check — the
	// hash lookup below never needs to return a real hash.
	modelDir := t.TempDir()
	weightsPath := filepath.Join(modelDir, "weights.bin")
	if err := os.WriteFile(weightsPath, []byte("weights"), 0600); err != nil {
		t.Fatalf("write weights file: %v", err)
	}

	getModelsForAdd = func(input *api.GetModelsInput) ([]*api.Model, error) {
		// hash never shows up
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "LOCAL",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: ""}},
		}}, nil
	}

	addModelOwner = "user-id"
	addModelName = "test-model"
	addModelDirectoryPath = modelDir
	addModelWaitForHash = true
	addModelDeleteAfterUpload = true
	addModelHashTimeout = 1 // nanoseconds; context.WithTimeout expires immediately

	cmd := newTestAddModelCommand()
	var runErr error
	captureStdStreams(t, func() {
		runErr = runAddModel(cmd, nil)
	})

	if runErr == nil {
		t.Fatal("expected non-zero error when hash confirmation fails")
	}
	if !strings.Contains(runErr.Error(), "timed out waiting for the model hash") {
		t.Fatalf("expected timeout error, got %v", runErr)
	}

	if _, err := os.Stat(weightsPath); err != nil {
		t.Fatalf("expected weights.bin to remain on disk when hash was never confirmed, stat err: %v", err)
	}
}

func TestDeleteVerifiedModelFilesReturnsNonZeroOnPartialFailure(t *testing.T) {
	oldRemove := removeModelFile
	t.Cleanup(func() { removeModelFile = oldRemove })

	var removed []string
	removeModelFile = func(path string) error {
		if strings.HasSuffix(path, "b.bin") {
			return errors.New("permission denied")
		}
		removed = append(removed, path)
		return nil
	}

	files := []modelFile{
		{AbsolutePath: "/tmp/model/a.bin", RelativePath: "a.bin", Size: 10},
		{AbsolutePath: "/tmp/model/b.bin", RelativePath: "b.bin", Size: 20},
		{AbsolutePath: "/tmp/model/c.bin", RelativePath: "c.bin", Size: 30},
	}

	deletedCount, deletedBytes, err := deleteVerifiedModelFiles(files)

	if err == nil {
		t.Fatal("expected error when one of the files fails to delete")
	}
	if !strings.Contains(err.Error(), "failed to delete 1 of 3 model file(s)") {
		t.Fatalf("expected partial-failure summary in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "b.bin: permission denied") {
		t.Fatalf("expected the specific failing file in the error, got %v", err)
	}
	if deletedCount != 2 {
		t.Fatalf("expected 2 successfully deleted files reported even on partial failure, got %d", deletedCount)
	}
	if deletedBytes != 40 {
		t.Fatalf("expected 40 deleted bytes (a.bin + c.bin), got %d", deletedBytes)
	}
	if len(removed) != 2 {
		t.Fatalf("expected exactly a.bin and c.bin to be removed, got %#v", removed)
	}
}

func TestDeleteVerifiedModelFilesSucceedsWhenAllRemoved(t *testing.T) {
	oldRemove := removeModelFile
	t.Cleanup(func() { removeModelFile = oldRemove })

	var removed []string
	removeModelFile = func(path string) error {
		removed = append(removed, path)
		return nil
	}

	files := []modelFile{
		{AbsolutePath: "/tmp/model/a.bin", RelativePath: "a.bin", Size: 5},
	}

	deletedCount, deletedBytes, err := deleteVerifiedModelFiles(files)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if deletedCount != 1 || deletedBytes != 5 {
		t.Fatalf("expected 1 file / 5 bytes deleted, got %d/%d", deletedCount, deletedBytes)
	}
	if len(removed) != 1 || removed[0] != "/tmp/model/a.bin" {
		t.Fatalf("expected removeModelFile called with absolute path, got %#v", removed)
	}
}
