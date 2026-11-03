package main

import (
	"os"

	"github.com/Vaspyyy/glint/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
