package main

import (
	"os"

	"github.com/revenuecat/cli/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Run(version))
}
