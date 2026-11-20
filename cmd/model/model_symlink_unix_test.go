//go:build unix

package model

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

// TestUpload_SwapToFIFOBetweenLstatAndOpenDoesNotHang swaps the regular file for
// a FIFO after openScannedModelFile's lstat passed and before its open. A
// blocking open would wait for a writer forever, before any HTTP stall guard
// exists; the nonblocking open plus the descriptor check must refuse it quickly.
func TestUpload_SwapToFIFOBetweenLstatAndOpenDoesNotHang(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scoped bool // pass the scan-time info (open beneath the scan root) or nil (open by path)
	}{{"beneath the scan root", true}, {"by path", false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "model.bin"), "original")
			files, err := collectModelFiles(root)
			if err != nil || len(files) != 1 {
				t.Fatalf("collect: %v %+v", err, files)
			}
			file := files[0]
			var scanned os.FileInfo
			if tc.scoped {
				scanned = file.info
			}

			orig := betweenLstatAndOpenHook
			t.Cleanup(func() { betweenLstatAndOpenHook = orig })
			swapped := false
			betweenLstatAndOpenHook = func(path string) {
				swapped = true
				if fifoErr := removeAndMkfifo(path); fifoErr != nil {
					t.Errorf("swap for a fifo: %v", fifoErr)
				}
			}

			type result struct {
				f   *os.File
				err error
			}
			done := make(chan result, 1)
			go func() {
				f, _, openErr := openScannedModelFile(file.uploadPath(), scanned)
				done <- result{f, openErr}
			}()
			select {
			case res := <-done:
				if !swapped {
					t.Fatal("hook did not run")
				}
				if res.err == nil {
					_ = res.f.Close() //nolint:errcheck // already failing the test; nothing to recover
					t.Fatal("expected the swapped-in FIFO to be refused")
				}
				if !strings.Contains(res.err.Error(), "regular file") {
					t.Fatalf("unexpected refusal: %v", res.err)
				}
			case <-time.After(5 * time.Second):
				// release a blocked open (a read-write open of a FIFO needs no peer) so
				// the goroutine does not outlive the test.
				if unblock, openErr := os.OpenFile(file.uploadPath(), os.O_RDWR, 0); openErr == nil {
					_ = unblock.Close() //nolint:errcheck // releasing a blocked open so the goroutine ends; nothing to recover
				}
				t.Fatal("open blocked on the FIFO swapped in after the lstat")
			}
		})
	}
}
