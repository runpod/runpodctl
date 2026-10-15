package main

import (
	"github.com/runpod/runpodctl/cmd"

	// Embed Mozilla CA certificates as fallback for environments without system certs
	// (e.g. minimal Docker images like ubuntu:22.04 without ca-certificates installed)
	_ "golang.org/x/crypto/x509roots/fallback"
)

// Version is set at build time via ldflags. release-please keeps this default
// in step with the latest release, which is what the nix build stamps.
// x-release-please-start-version
var Version = "v3.0.1"

// x-release-please-end

func main() {
	cmd.Execute(Version)
}
