package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml"
)

func TestCreateNewProject_WritesRunpodToml(t *testing.T) {
	tmpDir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	projectName := "test-project"
	createNewProject(projectName, "11.8.0", "3.10", "Hello_World", "", false)

	projectDir := filepath.Join(tmpDir, projectName)
	tomlPath := filepath.Join(projectDir, "runpod.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("expected runpod.toml: %v", err)
	}

	config, err := toml.LoadFile(tomlPath)
	if err != nil {
		t.Fatalf("load runpod.toml: %v", err)
	}
	runtimeTree, ok := config.Get("runtime").(*toml.Tree)
	if !ok || runtimeTree == nil {
		t.Fatalf("expected runtime section")
	}
	if got := runtimeTree.Get("python_version"); got != "3.10" {
		t.Fatalf("expected python_version 3.10, got %v", got)
	}

	handlerPath := filepath.Join(projectDir, "src", "handler.py")
	if _, err := os.Stat(handlerPath); err != nil {
		t.Fatalf("expected handler.py: %v", err)
	}
}
