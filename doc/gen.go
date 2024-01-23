package main

import (
	"log"

	"github.com/spf13/cobra/doc"
)

func main() {
	err := doc.GenMarkdownTree(cmd.rootCmd, "./doc/")
	if err != nil {
		log.Fatal(err)
	}
}
