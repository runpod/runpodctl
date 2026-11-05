//go:build unix

package nvtransfer

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUploadRefusesANonRegularFile(t *testing.T) {
	_, vol := setup(t)
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if _, err := vol.Upload(context.Background(), fifo, ""); !errors.As(err, new(*RefusedError)) {
		t.Fatalf("expected a refusal, got %v", err)
	}
}
