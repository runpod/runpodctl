package model

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/runpod/runpodctl/api"
)

func writeHiddenFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"weights.bin":                    "w",
		".gitattributes":                 "*.bin filter=lfs",
		".huggingface/config.json":       "{}",
		".git/config":                    "[core]",
		"sub/.git/HEAD":                  "ref",
		".env":                           "KEY=secret",
		".env.local":                     "KEY=secret",
		"sub/.env.production":            "KEY=secret",
		"envfile.txt":                    "not hidden",
		"sub/.environment-notes.md":      "not an env file",
		"nested/.envrc-like/readme.md":   "dir name merely resembles",
		"nested/.env.d/keep.txt":         "directory named .env.d is not a file",
		"nested/.git-keep-not-git/x.txt": "x",
	} {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(name)), content)
	}
	return root
}

func TestCollectModelFiles_SkipsGitAndEnvByDefault(t *testing.T) {
	root := writeHiddenFixture(t)

	files, skipped, err := collectModelFilesWithOptions(root, false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := strings.Join(collectedNames(files), ",")
	for _, banned := range []string{".git/config", "sub/.git/HEAD", ".env", ".env.local", "sub/.env.production"} {
		if containsName(files, banned) {
			t.Errorf("expected %q skipped, got %s", banned, got)
		}
	}
	for _, kept := range []string{"weights.bin", ".gitattributes", ".huggingface/config.json", "envfile.txt", "sub/.environment-notes.md", "nested/.envrc-like/readme.md", "nested/.env.d/keep.txt", "nested/.git-keep-not-git/x.txt"} {
		if !containsName(files, kept) {
			t.Errorf("expected %q kept, got %s", kept, got)
		}
	}
	// two .git dirs + .env + .env.local + sub/.env.production
	if skipped != 5 {
		t.Errorf("expected 5 skipped entries, got %d", skipped)
	}
}

func TestCollectModelFiles_IncludeHiddenKeepsEverything(t *testing.T) {
	root := writeHiddenFixture(t)

	files, skipped, err := collectModelFilesWithOptions(root, true)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if skipped != 0 {
		t.Errorf("expected nothing skipped with includeHidden, got %d", skipped)
	}
	for _, name := range []string{".git/config", "sub/.git/HEAD", ".env", ".env.local", "sub/.env.production", ".gitattributes"} {
		if !containsName(files, name) {
			t.Errorf("expected %q included, got %s", name, strings.Join(collectedNames(files), ","))
		}
	}
}

// TestCollectModelFiles_SkippedSymlinkIsNeverResolved pins that a skipped entry
// is decided by name before symlink resolution: a .env link pointing outside the
// model directory is skipped, not rejected as an escape.
func TestCollectModelFiles_SkippedSymlinkIsNeverResolved(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "weights.bin"), "w")
	outside := filepath.Join(t.TempDir(), "secret")
	writeTestFile(t, outside, "secret")
	symlinkOrSkip(t, outside, filepath.Join(root, ".env"))

	files, skipped, err := collectModelFilesWithOptions(root, false)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if skipped != 1 || strings.Join(collectedNames(files), ",") != "weights.bin" {
		t.Fatalf("unexpected result: skipped=%d files=%v", skipped, collectedNames(files))
	}
	if _, _, err := collectModelFilesWithOptions(root, true); err == nil {
		t.Fatalf("with --include-hidden the escaping link must still be refused")
	}
}

func containsName(files []modelFile, name string) bool {
	for _, f := range files {
		if f.RelativePath == name {
			return true
		}
	}
	return false
}

func TestIncludeHiddenFlagRegistered(t *testing.T) {
	if AddModelToRepoCmd.Flags().Lookup("include-hidden") == nil || addCmd.Flags().Lookup("include-hidden") == nil {
		t.Fatalf("--include-hidden must be registered on both add commands")
	}
}

// TestRunAddModel_HiddenFilesSkippedUnlessIncluded runs the real add flow and
// checks which names reach the upload batch and that the notice carries a count
// but no names.
func TestRunAddModel_HiddenFilesSkippedUnlessIncluded(t *testing.T) {
	for _, include := range []bool{false, true} {
		resetAddModelGlobals(t)
		stubAddModelUploadSeams(t)

		var sent []string
		inner := createModelRepoUploadBatch
		createModelRepoUploadBatch = func(input *api.CreateModelRepoUploadBatchInput) (*api.ModelRepoUploadBatchResult, error) {
			for _, f := range input.Files {
				sent = append(sent, f.FileName)
			}
			return inner(input)
		}

		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "weights.bin"), "w")
		writeTestFile(t, filepath.Join(dir, ".gitattributes"), "x")
		writeTestFile(t, filepath.Join(dir, ".git", "config"), "[core]")
		writeTestFile(t, filepath.Join(dir, ".env.local"), "KEY=secret")

		addModelOwner = "user-id"
		addModelName = "test-model"
		addModelDirectoryPath = dir
		addModelIncludeHidden = include

		_, stderr := captureStdStreams(t, func() {
			if err := runAddModel(newTestAddModelCommand(), nil); err != nil {
				t.Fatalf("runAddModel(include=%v): %v", include, err)
			}
		})

		joined := strings.Join(sent, ",")
		if include {
			if !strings.Contains(joined, ".git/config") || !strings.Contains(joined, ".env.local") {
				t.Errorf("include-hidden: expected hidden files uploaded, sent %s", joined)
			}
			if strings.Contains(stderr, "skipped") {
				t.Errorf("include-hidden: unexpected skip notice %q", stderr)
			}
			continue
		}
		if strings.Contains(joined, ".git/config") || strings.Contains(joined, ".env.local") {
			t.Errorf("default: sensitive files were uploaded: %s", joined)
		}
		if !strings.Contains(joined, ".gitattributes") || !strings.Contains(joined, "weights.bin") {
			t.Errorf("default: expected weights.bin and .gitattributes uploaded, sent %s", joined)
		}
		if !strings.Contains(stderr, "skipped 2 sensitive entries") || strings.Contains(stderr, "KEY=secret") || strings.Contains(stderr, ".env.local") {
			t.Errorf("default: unexpected notice %q", stderr)
		}
	}
}
