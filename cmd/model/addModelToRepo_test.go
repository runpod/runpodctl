package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectModelFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("root"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}

	nestedDir := filepath.Join(dir, "nested")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(nestedDir, "child.bin"), []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	files, err := collectModelFiles(dir)
	if err != nil {
		t.Fatalf("collectModelFiles returned error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	if files[0].RelativePath != "nested/child.bin" {
		t.Fatalf("expected first relative path nested/child.bin, got %s", files[0].RelativePath)
	}
	if files[1].RelativePath != "root.txt" {
		t.Fatalf("expected second relative path root.txt, got %s", files[1].RelativePath)
	}

	if files[0].AbsolutePath != filepath.Join(nestedDir, "child.bin") {
		t.Fatalf("unexpected absolute path for first file: %s", files[0].AbsolutePath)
	}
	if files[1].AbsolutePath != filepath.Join(dir, "root.txt") {
		t.Fatalf("unexpected absolute path for second file: %s", files[1].AbsolutePath)
	}

	if files[0].Size != 3 {
		t.Fatalf("expected size 3 for nested file, got %d", files[0].Size)
	}
	if files[1].Size != 4 {
		t.Fatalf("expected size 4 for root file, got %d", files[1].Size)
	}
}

func TestCollectModelFilesIgnoresEmptyDirectories(t *testing.T) {
	dir := t.TempDir()

	nestedDir := filepath.Join(dir, "empty")
	if err := os.Mkdir(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	files, err := collectModelFiles(dir)
	if err != nil {
		t.Fatalf("collectModelFiles returned error: %v", err)
	}

	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}
