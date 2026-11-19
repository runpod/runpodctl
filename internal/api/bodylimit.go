package api

import (
	"fmt"
	"io"
)

// MaxResponseBytes bounds how much of an api response body the cli buffers. It
// is far above any real response (the largest are model and pod listings, and
// serverless sync output, which the platform caps near 20 MB); it exists so a
// hostile or broken server cannot make the cli hold hundreds of MB.
const MaxResponseBytes = 32 << 20

// ReadBounded reads body up to MaxResponseBytes and fails if the server sent
// more, rather than silently truncating into invalid json.
func ReadBounded(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, MaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d MiB limit", MaxResponseBytes>>20)
	}
	return data, nil
}
