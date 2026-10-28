//go:build e2e

package e2e

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLI_NetworkVolumeTransfer round-trips a multipart file and a folder
// through a fresh network volume, then checks a rerun skips everything. It
// needs s3 api keys, and creates (and deletes) a billed 10 gb volume, so it
// only runs with RUNPOD_E2E_VOLUMES=1.
func TestCLI_NetworkVolumeTransfer(t *testing.T) {
	if os.Getenv("RUNPOD_E2E_VOLUMES") != "1" || os.Getenv("RUNPOD_S3_ACCESS_KEY") == "" || os.Getenv("RUNPOD_S3_SECRET_KEY") == "" {
		t.Skip("set RUNPOD_E2E_VOLUMES=1 and RUNPOD_S3_ACCESS_KEY/RUNPOD_S3_SECRET_KEY to run")
	}
	volumeID := createTestVolume(t)

	src := t.TempDir()
	writeRandom(t, filepath.Join(src, "big.bin"), 150<<20) // over one 64 mib part
	writeRandom(t, filepath.Join(src, "sub", "small.txt"), 1<<10)

	runTransfer(t, "upload", volumeID, src, "data")
	dst := filepath.Join(t.TempDir(), "data")
	runTransfer(t, "download", volumeID, "data/", dst)

	for _, rel := range []string{"big.bin", filepath.Join("sub", "small.txt")} {
		if digest(t, filepath.Join(src, rel)) != digest(t, filepath.Join(dst, rel)) {
			t.Fatalf("%s differs after the round trip", rel)
		}
	}

	if sum := runTransfer(t, "upload", volumeID, src, "data"); sum.FilesTransferred != 0 || sum.FilesSkipped != 2 {
		t.Fatalf("a rerun should skip both files, got %+v", sum)
	}
}

type transferSummary struct {
	FilesTransferred int `json:"filesTransferred"`
	FilesSkipped     int `json:"filesSkipped"`
}

func createTestVolume(t *testing.T) string {
	t.Helper()
	out, stderr, err := runCLI("network-volume", "create", "--name", "e2e-transfer", "--size", "10", "--data-center-id", "EU-RO-1")
	if err != nil {
		t.Fatalf("create volume: %v\n%s", err, stderr)
	}
	var volume struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &volume); err != nil || volume.ID == "" {
		t.Fatalf("create volume: unexpected output %q", out)
	}
	t.Cleanup(func() {
		if _, stderr, err := runCLI("network-volume", "delete", volume.ID); err != nil {
			t.Errorf("delete volume %s: %v\n%s", volume.ID, err, stderr)
		}
	})
	return volume.ID
}

func runTransfer(t *testing.T, direction, volumeID, from, to string) transferSummary {
	t.Helper()
	out, stderr, err := runCLI("network-volume", direction, volumeID, from, to)
	if err != nil {
		t.Fatalf("%s: %v\n%s", direction, err, stderr)
	}
	var sum transferSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &sum); err != nil {
		t.Fatalf("%s: unexpected output %q", direction, out)
	}
	return sum
}

func writeRandom(t *testing.T, path string, size int) {
	t.Helper()
	data := make([]byte, size)
	rand.Read(data)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func digest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return string(sum[:])
}
