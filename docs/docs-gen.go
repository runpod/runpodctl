package main

import (
	"log"

	"github.com/runpod/runpodctl/cmd"

	"github.com/spf13/cobra/doc"
)

func main() {
	rootCmd := cmd.GetRootCmd()
	// drop the "Auto generated ... on <date>" footer so a regen only touches
	// docs with real content changes, instead of rewriting the date in every file.
	rootCmd.DisableAutoGenTag = true
	err := doc.GenMarkdownTree(rootCmd, "./docs/")
	if err != nil {
		log.Fatal(err)
	}
}
