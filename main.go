package main

import (
	"strings"

	"github.com/runpod/runpodctl/cmd"
)

// Version is set at build time via ldflags (see makefile and .goreleaser.yml)
// If not set, falls back to "dev"
var Version string

func main() {
	version := Version
	if version == "" {
		version = "local-dev"
	}
	cmd.Execute(strings.TrimRight(version, "\r\n"))
}
