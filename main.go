package main

import (
	_ "embed"
	"strings"

	"github.com/runpod/runpodctl/cmd"
)

//go:embed version
var Version string

func main() {
	cmd.Execute(strings.TrimRight(Version, "\r\n"))
}
