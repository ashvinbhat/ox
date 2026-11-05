// Package livepath is how ox finds the user's home, and what keeps tests off
// it: under go test, a path outside the temp dir panics, so a test can't read
// or write the real ~/.ox, ~/.claude or the desktop app's state.
package livepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Home is os.UserHomeDir, guarded.
func Home() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return Guard(home), nil
}

// Guard returns path. Under go test it panics unless path is inside the temp
// dir: point HOME, OX_HOME or CLAUDE_CONFIG_DIR at t.TempDir() first.
func Guard(path string) string {
	if testing.Testing() && !inside(path, os.TempDir()) {
		panic(fmt.Sprintf("a test reached the live path %s: point HOME, OX_HOME or CLAUDE_CONFIG_DIR at t.TempDir() first", path))
	}
	return path
}

func inside(path, dir string) bool {
	rel, err := filepath.Rel(resolve(dir), resolve(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// resolve follows symlinks so /var and /private/var compare equal on macOS.
// For a path that doesn't exist yet, it resolves the nearest parent that does.
func resolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	dir, rest := abs, ""
	for range strings.Count(abs, string(filepath.Separator)) + 1 {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = filepath.Dir(dir)
	}
	return abs
}
