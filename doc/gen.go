package main

import (
	"cli/cmd"
	"log"

	"github.com/spf13/cobra/doc"
)

func main() {
	err := doc.GenMarkdownTree(cmd.RootCmd, "./doc/")
	if err != nil {
		log.Fatal(err)
	}
}
