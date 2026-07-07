// Package version holds the ox build version, injected at build time via
// -ldflags "-X github.com/ashvinbhat/ox/internal/version.Version=...".
package version

// Version is the build version. Overridden at build time; defaults to "dev"
// for local `go build`/`go run` invocations.
var Version = "dev"
