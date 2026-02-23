package main

import "github.com/runpod/runpodctl/cmd"

// Version is set at build time via ldflags
var Version = "v2.0.0-beta.1"

func main() {
	cmd.Execute(Version)
}
