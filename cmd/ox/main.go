package main

import (
	"os"

	"github.com/ashvinbhat/ox/internal/cli"
)

// version is stamped at build time via -ldflags "-X main.version=vX.Y.Z";
// defaults to "dev" for local builds.
var version = "dev"

func main() {
	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
