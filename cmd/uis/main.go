package main

import (
	"os"

	"uis-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
