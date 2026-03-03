package main

import (
	"github.com/runpod/runpodctl/cmd"

	// Embed Mozilla CA certificates as fallback for environments without system certs
	// (e.g. minimal Docker images like ubuntu:22.04 without ca-certificates installed)
	_ "golang.org/x/crypto/x509roots/fallback"
)

// Version is set at build time via ldflags
var Version = "v2.0.0-beta.1"

func main() {
	cmd.Execute(Version)
}
