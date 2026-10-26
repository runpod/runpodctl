package nvtransfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// uploadState is what a rerun needs to continue a multipart upload. Size and
// mtime pin it to one version of the local file.
type uploadState struct {
	UploadID string `json:"uploadId"`
	PartSize int64  `json:"partSize"`
	FileSize int64  `json:"fileSize"`
	MtimeNs  int64  `json:"mtimeNs"`
}

// downloadState sits next to the partial file and lists the chunks already
// written and synced. Size and etag pin it to one version of the remote object.
type downloadState struct {
	Size      int64  `json:"size"`
	ETag      string `json:"etag"`
	ChunkSize int64  `json:"chunkSize"`
	Done      []int  `json:"done"`
}

// stateDir holds upload state files. Tests point it at a temp dir.
var stateDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".runpod", "transfers"), nil
}

func uploadStatePath(endpoint, volumeID, key, localPath string) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(endpoint + "|" + volumeID + "|" + key + "|" + localPath))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}

// readJSON returns false with no error when the file does not exist.
func readJSON(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		// a torn or foreign file is treated as no state
		return false, nil
	}
	return true, nil
}

// writeJSON writes atomically so a crash never leaves a half-written state.
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
