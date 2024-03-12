package main

import (
	"log"

	"github.com/runpod/runpodctl/cmd"

	"github.com/spf13/cobra/doc"
)

func main() {
	rootCmd := cmd.GetRootCmd()
	err := doc.GenMarkdownTree(rootCmd, "./docs/")
	if err != nil {
		log.Fatal(err)
	}
}
