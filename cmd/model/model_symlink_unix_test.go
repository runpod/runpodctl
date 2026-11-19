//go:build unix

package model

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCollectModelFiles_FIFORefused(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "ok.bin"), "ok")
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	if _, err := collectModelFiles(root); err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("expected a FIFO to be refused, got %v", err)
	}
}

func TestCollectModelFiles_SymlinkToFIFORefused(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	symlinkOrSkip(t, "pipe", filepath.Join(root, "link"))

	if _, err := collectModelFiles(root); err == nil {
		t.Fatal("expected a FIFO (directly or via a link) to be refused")
	}
}

func TestUpload_SwapToFIFOAfterScanRefusedWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "model.bin"), "original")
	files, err := collectModelFiles(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("collect: %v %+v", err, files)
	}
	file := files[0]
	if fifoErr := removeAndMkfifo(file.ResolvedPath); fifoErr != nil {
		t.Skipf("mkfifo unavailable: %v", fifoErr)
	}

	server, hits := newUnreachableUploadServer(t)
	err = completeModelUploadFile(t.Context(), uploadFor(server.URL), file.uploadPath(), file.info, nil)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("expected the swapped-in FIFO to be refused, got %v", err)
	}
	if hits() != 0 {
		t.Fatalf("server saw %d request(s)", hits())
	}
}

func removeAndMkfifo(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syscall.Mkfifo(path, 0o600)
}
