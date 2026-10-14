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

func TestNeedsRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write any directory")
	}
	dir := t.TempDir()
	// install writes exe into dir, then optionally marks it a conda prefix and
	// locks the bin directory.
	install := func(name string, conda, locked bool) string {
		bin := filepath.Join(dir, name, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if conda {
			if err := os.Mkdir(filepath.Join(dir, name, "conda-meta"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		exe := filepath.Join(bin, "runpodctl")
		if err := os.WriteFile(exe, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if locked {
			if err := os.Chmod(bin, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(bin, 0o755) })
		}
		return exe
	}

	for _, tc := range []struct {
		description string
		exe         string
		expected    bool
	}{
		{"direct install, writable dir", install("writable", false, false), false},
		{"direct install, unwritable dir", install("locked", false, true), true},
		{"package manager, unwritable dir", install("conda", true, true), false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := needsRoot(tc.exe); got != tc.expected {
				t.Errorf("needsRoot(%q) = %t, expected %t", tc.exe, got, tc.expected)
			}
		})
	}
}
