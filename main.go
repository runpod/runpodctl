package main

import (
	_ "embed"

	"github.com/runpod/runpodctl/cmd"
)

//go:embed version
var Version string

func main() {
	cmd.Execute(Version)
}
