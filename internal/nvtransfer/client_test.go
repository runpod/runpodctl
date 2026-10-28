package nvtransfer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestEndpointURL(t *testing.T) {
	if got := EndpointURL(" EU-RO-1 "); got != "https://s3api-eu-ro-1.runpod.io/" {
		t.Fatalf("got %s", got)
	}
}

// runpod rewrites Accept-Encoding before checking signatures, so the header
// must be sent but never signed.
func TestAcceptEncodingSentButNotSigned(t *testing.T) {
	var auth, acceptEncoding, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		acceptEncoding = r.Header.Get("Accept-Encoding")
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<ListBucketResult></ListBucketResult>`))
	}))
	defer srv.Close()

	client := NewS3Client(srv.URL+"/", "EU-RO-1", "user_x", "secret")
	if _, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String("vol1")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auth, "SignedHeaders=") || strings.Contains(auth, "accept-encoding") {
		t.Fatalf("accept-encoding must not be signed: %s", auth)
	}
	if acceptEncoding != "identity" {
		t.Fatalf("accept-encoding not sent, got %q", acceptEncoding)
	}
	if path != "/vol1" {
		t.Fatalf("expected path-style /vol1, got %q", path)
	}
}

// the client retries the 524 runpod's proxy returns on a slow operation.
func TestClientRetries524(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 {
			w.WriteHeader(524)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(`<ListBucketResult></ListBucketResult>`))
	}))
	defer srv.Close()

	client := NewS3Client(srv.URL+"/", "EU-RO-1", "user_x", "secret")
	if _, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String("vol1")}); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatalf("expected two 524s then success, got %d requests", hits.Load())
	}
}
