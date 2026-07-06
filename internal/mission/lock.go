package mission

import (
	"fmt"
	"os"
	"syscall"
)

// WithLock serializes read-modify-write access to one mission's files across
// processes (CLI, MCP servers, watcher).
func WithLock(oxHome, id string, fn func() error) error {
	dir, err := findDir(oxHome, id)
	if err != nil {
		return err
	}
	return withFileLock(dir+"/.lock", fn)
}

// WithFileLock serializes fn across processes on an arbitrary lock file.
func WithFileLock(path string, fn func() error) error {
	return withFileLock(path, fn)
}

func withFileLock(path string, fn func() error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}
