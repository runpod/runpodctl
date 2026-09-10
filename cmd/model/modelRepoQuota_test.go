package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/api"
)

func strPtr(s string) *string { return &s }

func TestCheckModelRepoStorageQuotaAllowsWhenNotEnforced(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		return &api.ModelRepoStorageUsage{
			Enforced:       false,
			AvailableBytes: strPtr("0"),
		}, nil
	}

	if err := checkModelRepoStorageQuota("owner", 1_000_000); err != nil {
		t.Fatalf("expected no error when enforcement is disabled, got %v", err)
	}
}

func TestCheckModelRepoStorageQuotaAllowsWhenNoAvailableBytes(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		return &api.ModelRepoStorageUsage{Enforced: true, AvailableBytes: nil}, nil
	}

	if err := checkModelRepoStorageQuota("owner", 1_000_000); err != nil {
		t.Fatalf("expected no error when no quota is configured, got %v", err)
	}
}

func TestCheckModelRepoStorageQuotaAllowsWhenUnderLimit(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	var sawOwner string
	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		sawOwner = owner
		return &api.ModelRepoStorageUsage{Enforced: true, AvailableBytes: strPtr("1000")}, nil
	}

	if err := checkModelRepoStorageQuota("my-owner", 999); err != nil {
		t.Fatalf("expected no error when requestedBytes is under the limit, got %v", err)
	}
	if sawOwner != "my-owner" {
		t.Fatalf("expected owner to be forwarded to getModelRepoStorageUsage, got %q", sawOwner)
	}
}

func TestCheckModelRepoStorageQuotaBlocksWhenOverLimit(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		return &api.ModelRepoStorageUsage{Enforced: true, AvailableBytes: strPtr("1000")}, nil
	}

	err := checkModelRepoStorageQuota("owner", 1001)
	if err == nil {
		t.Fatal("expected an error when requestedBytes exceeds availableBytes")
	}
	if !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("expected a helpful quota-exceeded message, got %v", err)
	}
}

func TestCheckModelRepoStorageQuotaAllowsExactlyAtLimit(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		return &api.ModelRepoStorageUsage{Enforced: true, AvailableBytes: strPtr("1000")}, nil
	}

	if err := checkModelRepoStorageQuota("owner", 1000); err != nil {
		t.Fatalf("expected requestedBytes == availableBytes to be allowed, got %v", err)
	}
}

func TestCheckModelRepoStorageQuotaFailsOpenOnQueryError(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		return nil, errors.New("cannot query field \"modelRepoStorageUsage\" on type \"Query\"")
	}

	if err := checkModelRepoStorageQuota("owner", 1_000_000_000); err != nil {
		t.Fatalf("expected the quota check to fail open on a query error, got %v", err)
	}
}

func TestCheckModelRepoStorageQuotaFailsOpenOnUnparseableAvailableBytes(t *testing.T) {
	oldGetUsage := getModelRepoStorageUsage
	t.Cleanup(func() { getModelRepoStorageUsage = oldGetUsage })

	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		return &api.ModelRepoStorageUsage{Enforced: true, AvailableBytes: strPtr("not-a-number")}, nil
	}

	if err := checkModelRepoStorageQuota("owner", 1); err != nil {
		t.Fatalf("expected the quota check to fail open on an unparseable response, got %v", err)
	}
}

func TestModelUploadRequestedBytesFromDirectory(t *testing.T) {
	files := []modelFile{
		{RelativePath: "a.bin", Size: 100},
		{RelativePath: "b.bin", Size: 250},
	}

	bytes, ok := modelUploadRequestedBytes(files)
	if !ok {
		t.Fatal("expected ok=true when modelFiles is non-empty")
	}
	if bytes != 350 {
		t.Fatalf("expected 350 total bytes, got %d", bytes)
	}
}

func TestModelUploadRequestedBytesFromFileSizeFlag(t *testing.T) {
	oldFileSize := addModelFileSize
	t.Cleanup(func() { addModelFileSize = oldFileSize })
	addModelFileSize = "12345"

	bytes, ok := modelUploadRequestedBytes(nil)
	if !ok {
		t.Fatal("expected ok=true when --file-size parses as an integer")
	}
	if bytes != 12345 {
		t.Fatalf("expected 12345 bytes, got %d", bytes)
	}
}

func TestModelUploadRequestedBytesUnparseableFileSize(t *testing.T) {
	oldFileSize := addModelFileSize
	t.Cleanup(func() { addModelFileSize = oldFileSize })
	addModelFileSize = "not-a-number"

	if _, ok := modelUploadRequestedBytes(nil); ok {
		t.Fatal("expected ok=false when --file-size cannot be parsed")
	}
}

func TestRunAddModelFastFailsBeforeCreatingModelWhenOverQuota(t *testing.T) {
	resetAddModelGlobals(t)
	oldAddModelToRepo := addModelToRepo
	oldCreateModelRepoUpload := createModelRepoUpload
	oldGetModelRepoStorageUsage := getModelRepoStorageUsage
	t.Cleanup(func() {
		addModelToRepo = oldAddModelToRepo
		createModelRepoUpload = oldCreateModelRepoUpload
		getModelRepoStorageUsage = oldGetModelRepoStorageUsage
	})

	modelDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(modelDir, "weights.bin"), make([]byte, 2000), 0600); err != nil {
		t.Fatalf("write model file: %v", err)
	}

	addModelOwner = "user-id"
	addModelName = "test-model"
	addModelDirectoryPath = modelDir

	addModelToRepoCalled := false
	addModelToRepo = func(*api.AddModelToRepoInput) (*api.Model, error) {
		addModelToRepoCalled = true
		return &api.Model{ID: "model-id"}, nil
	}
	createModelRepoUploadCalled := false
	createModelRepoUpload = func(*api.CreateModelRepoUploadInput) (*api.ModelRepoMutationResult, error) {
		createModelRepoUploadCalled = true
		return nil, errors.New("should not be called")
	}
	getModelRepoStorageUsage = func(owner string) (*api.ModelRepoStorageUsage, error) {
		if owner != "user-id" {
			t.Errorf("expected quota check for owner user-id, got %q", owner)
		}
		return &api.ModelRepoStorageUsage{Enforced: true, AvailableBytes: strPtr("1000")}, nil
	}

	err := runAddModel(newTestAddModelCommand(), nil)
	if err == nil {
		t.Fatal("expected an over-quota error")
	}
	if !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("expected a helpful quota message, got %v", err)
	}
	if addModelToRepoCalled {
		t.Error("addModelToRepo must not be called when the fast-fail quota check rejects the upload")
	}
	if createModelRepoUploadCalled {
		t.Error("createModelRepoUpload must not be called when the fast-fail quota check rejects the upload")
	}
}
