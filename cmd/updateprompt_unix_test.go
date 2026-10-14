//go:build unix

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write any directory")
	}
	dir := t.TempDir()
	if !dirWritable(dir) {
		t.Fatalf("dirWritable(%q) = false, want true", dir)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	if dirWritable(locked) {
		t.Fatalf("dirWritable(%q) = true, want false", locked)
	}
}
