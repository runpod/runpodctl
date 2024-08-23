// configure-github-action is a small tool that generates a Dockerfile and endpoint.toml file from a runpod.toml file.
// If you already have those two, this is a no-op.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type EndpointConfig struct{}

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	log.SetPrefix("configure-github-action: ")
	log.Printf("start: dir=%s", dir)
	if err := build(context.TODO(), dir); err != nil {
		log.Fatalf("fail: %v", err)
	}
	log.Printf("done")
}

// if they have dockerfile+endpoint.toml, use that
// otherwise, use runpod.toml to generate the dockerfile
func build(ctx context.Context, dir string) error {
	exists := func(path string) bool {
		_, err := os.Stat(filepath.Join(".", path))
		return err == nil
	}
	// runpodctl commmands only work on current directory, so we chdir
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	switch { // bounds checks
	case !exists("runpod.toml") && !exists("Dockerfile") && !exists("endpoint.toml"):
		return errors.New("no runpod.toml, Dockerfile, or endpoint.toml found... did you choose the right directory?")
	case exists("runpod.toml") && (exists("Dockerfile") || exists("endpoint.toml")):
		return errors.New("expected no Dockerfile or endpoint.toml when runpod.toml is present")
	case exists("runpod.toml"):
		log.Print("found runpod.toml")

		for _, cmdAndArgs := range [][]string{
			{"runpodctl", "project", "generate-endpoint-config"},
			{"runpodctl", "project", "build-dockerfile"},
		} {
			log.Printf("start %v", strings.Join(cmdAndArgs, " "))
			cmd := exec.CommandContext(ctx, cmdAndArgs[0], cmdAndArgs[1:]...)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%v: %v", strings.Join(cmdAndArgs, " "), err)
			}
		}
		log.Printf("generated Dockerfile and endpoint.toml")
		return nil
	case exists("Dockerfile") && exists("endpoint.toml"):
		log.Print("found pre-existing Dockerfile and endpoint.toml")
		return nil
	default:
		return errors.New("expected either 'runpod.toml' or both 'Dockerfile' and 'endpoint.toml' to exist")
	}
}
