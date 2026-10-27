package main

import (
	"os"

	"github.com/ashvinbhat/ox/internal/cli"
	"github.com/ashvinbhat/ox/internal/version"
)

func main() {
	cli.SetVersion(version.Version)
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
