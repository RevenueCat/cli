package main

import (
	"os"

	"github.com/revenuecat/cli/internal/buildinfo"
	"github.com/revenuecat/cli/internal/cli"
)

var version = "dev"

func main() {
	buildinfo.Version = version
	os.Exit(cli.Run(version))
}
