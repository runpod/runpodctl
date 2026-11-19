package model

import (
	"context"
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
	completeModelUploadFile = func(_ context.Context, _ *api.ModelRepoUpload, _ string, _ os.FileInfo, _ modelUploadProgress) error {
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
		// the real hash never shows up: the version keeps the placeholder hash the
		// api assigns at creation, which is exactly the state that must NOT count
		// as confirmation for deleting local files.
		return []*api.Model{{
			ID:       "model-id",
			Owner:    "user-id",
			Name:     "test-model",
			Provider: "LOCAL",
			Versions: []*api.ModelVersion{{UUID: "version-uuid", Hash: "ph-0123456789abcdef", Status: api.ModelVersionStatusNeedsHash}},
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
	if !strings.Contains(runErr.Error(), "timed out waiting for the model version to become deployable") {
		t.Fatalf("expected timeout error, got %v", runErr)
	}

	if _, err := os.Stat(weightsPath); err != nil {
		t.Fatalf("expected weights.bin to remain on disk when the version never became deployable, stat err: %v", err)
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

	deletedCount, deletedBytes, _, err := deleteVerifiedModelFiles(files)

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

	deletedCount, deletedBytes, _, err := deleteVerifiedModelFiles(files)
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

// deleting a symlinked entry removes the link only; the target (an HF blob)
// stays, and its size must not be reported as freed bytes.
func TestDeleteVerifiedModelFilesCountsSymlinksSeparately(t *testing.T) {
	cache := t.TempDir()
	repo := filepath.Join(cache, "models--owner--name")
	blob := filepath.Join(repo, "blobs", "sha-weights")
	writeTestFile(t, blob, "weights-bytes")
	snapshot := filepath.Join(repo, "snapshots", "rev")
	writeTestFile(t, filepath.Join(snapshot, "config.json"), "cfg")
	symlinkOrSkip(t, filepath.Join("..", "..", "blobs", "sha-weights"), filepath.Join(snapshot, "model.safetensors"))

	files, err := collectModelFiles(snapshot)
	if err != nil || len(files) != 2 {
		t.Fatalf("collect: %v %+v", err, files)
	}

	oldDir := addModelDirectoryPath
	addModelDirectoryPath = snapshot
	t.Cleanup(func() { addModelDirectoryPath = oldDir })

	var deletedCount, removedLinks int
	var deletedBytes int64
	_, stderr := captureStdStreams(t, func() {
		var delErr error
		deletedCount, deletedBytes, removedLinks, delErr = deleteVerifiedModelFiles(files)
		if delErr != nil {
			t.Fatalf("delete: %v", delErr)
		}
	})

	if deletedCount != 1 || deletedBytes != int64(len("cfg")) {
		t.Fatalf("only the regular file counts as deleted: got %d file(s), %d bytes", deletedCount, deletedBytes)
	}
	if removedLinks != 1 {
		t.Fatalf("expected 1 symlink removed, got %d", removedLinks)
	}
	if _, err := os.Lstat(filepath.Join(snapshot, "model.safetensors")); !os.IsNotExist(err) {
		t.Fatalf("the link should be removed, lstat err: %v", err)
	}
	got, readErr := os.ReadFile(blob) //nolint:gosec // G304: test-controlled temp path
	if readErr != nil || string(got) != "weights-bytes" {
		t.Fatalf("the link target must be kept: %q, %v", got, readErr)
	}
	if !strings.Contains(stderr, "deleted 1 verified model file(s) (3 bytes)") ||
		!strings.Contains(stderr, "removed 1 symlink(s) (targets kept)") {
		t.Fatalf("stderr should report files and symlinks separately, got %q", stderr)
	}
}
