package main

import (
	"cli/cmd"
	"log"

	"github.com/spf13/cobra/doc"
)

func main() {
	rootCmd := cmd.GetRootCmd()
	err := doc.GenMarkdownTree(rootCmd, "./docs/")
	if err != nil {
		log.Fatal(err)
	}
}
