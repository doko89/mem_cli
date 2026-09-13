package main

import (
	"os"

	"mem_cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
