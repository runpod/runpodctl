package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestChecksumAssetName(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "tag with leading v", version: "v2.6.1", want: "checksums_2.6.1_sha256.txt"},
		{name: "version without leading v", version: "2.6.1", want: "checksums_2.6.1_sha256.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checksumAssetName(tt.version); got != tt.want {
				t.Fatalf("checksumAssetName(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestFindAsset(t *testing.T) {
	assets := []Asset{
		{Name: "runpodctl-linux-amd64.tar.gz", Url: "https://example.test/archive"},
		{Name: "checksums_2.6.1_sha256.txt", Url: "https://example.test/checksums"},
	}

	asset, ok := findAsset(assets, "checksums_2.6.1_sha256.txt")
	if !ok {
		t.Fatal("expected checksum asset to be found")
	}
	if asset.Url != "https://example.test/checksums" {
		t.Fatalf("asset.Url = %q, want checksum URL", asset.Url)
	}

	if _, ok := findAsset(assets, "missing.txt"); ok {
		t.Fatal("expected missing asset lookup to fail")
	}
}

func TestChecksumForAsset(t *testing.T) {
	const linuxArchive = "runpodctl-linux-amd64.tar.gz"
	const linuxDigest = "0fbcbfad3706995370b7858ee08ad7faa9579a639d831655b0a7ebe6149782c2"
	checksumText := []byte(strings.Join([]string{
		"09001e4666934c338919b00bc5e415259fe93228b5eb56f51c141730d5e5bbee  runpodctl-darwin-all.tar.gz",
		linuxDigest + "  " + linuxArchive,
		"a18a5dd9d0ac33f91399bd1d1bca09fbb95b6a82ffb3722f2528e3daee94f50c  runpodctl-windows-amd64.zip",
	}, "\n"))

	got, err := checksumForAsset(checksumText, linuxArchive)
	if err != nil {
		t.Fatalf("checksumForAsset returned error: %v", err)
	}
	if got != linuxDigest {
		t.Fatalf("checksumForAsset digest = %q, want %q", got, linuxDigest)
	}
}

func TestChecksumForAssetMissing(t *testing.T) {
	_, err := checksumForAsset([]byte("09001e4666934c338919b00bc5e415259fe93228b5eb56f51c141730d5e5bbee  runpodctl-darwin-all.tar.gz\n"), "runpodctl-linux-amd64.tar.gz")
	if err == nil {
		t.Fatal("expected missing checksum entry to fail")
	}
	if !strings.Contains(err.Error(), "checksum not found") {
		t.Fatalf("expected missing checksum error, got %v", err)
	}
}

func TestChecksumForAssetMalformedDigest(t *testing.T) {
	_, err := checksumForAsset([]byte("not-a-sha  runpodctl-linux-amd64.tar.gz\n"), "runpodctl-linux-amd64.tar.gz")
	if err == nil {
		t.Fatal("expected malformed checksum entry to fail")
	}
	if !strings.Contains(err.Error(), "invalid checksum") {
		t.Fatalf("expected invalid checksum error, got %v", err)
	}
}

func TestVerifyFileChecksum(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "archive-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("release archive bytes"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256([]byte("release archive bytes"))
	expected := fmt.Sprintf("%x", digest)

	if err := verifyFileChecksum(file.Name(), expected); err != nil {
		t.Fatalf("verifyFileChecksum returned error: %v", err)
	}

	if err := verifyFileChecksum(file.Name(), strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected checksum mismatch to fail")
	}
}

func TestVerifyArchiveChecksum(t *testing.T) {
	archive, err := os.CreateTemp(t.TempDir(), "runpodctl-linux-amd64-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.WriteString("archive bytes"); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256([]byte("archive bytes"))
	checksumText := []byte(fmt.Sprintf("%x  runpodctl-linux-amd64.tar.gz\n", digest))

	if err := verifyArchiveChecksum(archive.Name(), "runpodctl-linux-amd64.tar.gz", checksumText); err != nil {
		t.Fatalf("verifyArchiveChecksum returned error: %v", err)
	}
}

func TestVerifyArchiveChecksumFailsWhenEntryMissing(t *testing.T) {
	archive, err := os.CreateTemp(t.TempDir(), "runpodctl-linux-amd64-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	err = verifyArchiveChecksum(archive.Name(), "runpodctl-linux-amd64.tar.gz", []byte("09001e4666934c338919b00bc5e415259fe93228b5eb56f51c141730d5e5bbee  runpodctl-darwin-all.tar.gz\n"))
	if err == nil {
		t.Fatal("expected missing checksum to fail")
	}
	if !strings.Contains(err.Error(), "checksum not found") {
		t.Fatalf("expected missing checksum error, got %v", err)
	}
}
