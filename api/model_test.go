package api

import (
	"encoding/json"
	"testing"
)

func TestModelRepoUploadUnmarshalJSON_StringPartSize(t *testing.T) {
	payload := []byte(`{
                "uploadId": "upload",
                "bucket": "bucket",
                "key": "key",
                "keyPrefix": "prefix",
                "partSizeBytes": "5242880",
                "partCount": 2,
                "expiresInSeconds": 60,
                "parts": [],
                "completeUrl": "complete",
                "abortUrl": "abort"
        }`)

	var upload ModelRepoUpload
	if err := json.Unmarshal(payload, &upload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upload.PartSizeBytes != 5242880 {
		t.Fatalf("expected partSizeBytes to be 5242880, got %d", upload.PartSizeBytes)
	}
}

func TestModelRepoUploadUnmarshalJSON_NumberPartSize(t *testing.T) {
	payload := []byte(`{
                "uploadId": "upload",
                "bucket": "bucket",
                "key": "key",
                "keyPrefix": "prefix",
                "partSizeBytes": 4096,
                "partCount": 2,
                "expiresInSeconds": 60,
                "parts": [],
                "completeUrl": "complete",
                "abortUrl": "abort"
        }`)

	var upload ModelRepoUpload
	if err := json.Unmarshal(payload, &upload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upload.PartSizeBytes != 4096 {
		t.Fatalf("expected partSizeBytes to be 4096, got %d", upload.PartSizeBytes)
	}
}

func TestModelRepoUploadUnmarshalJSON_InvalidString(t *testing.T) {
	payload := []byte(`{
                "uploadId": "upload",
                "bucket": "bucket",
                "key": "key",
                "keyPrefix": "prefix",
                "partSizeBytes": "invalid",
                "partCount": 2,
                "expiresInSeconds": 60,
                "parts": [],
                "completeUrl": "complete",
                "abortUrl": "abort"
        }`)

	var upload ModelRepoUpload
	if err := json.Unmarshal(payload, &upload); err == nil {
		t.Fatal("expected error when decoding invalid partSizeBytes, got nil")
	}
}
