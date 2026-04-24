package filelock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileLock represents a file ownership claim by an agent.
type FileLock struct {
	Pattern  string    `json:"pattern"`
	AgentID  string    `json:"agent_id"`
	LockedAt time.Time `json:"locked_at"`
}

// LockFile holds all file locks for a task.
type LockFile struct {
	Locks []FileLock `json:"locks"`
}

// Manager manages file ownership locks.
type Manager struct {
	path string // path to locks.json
}

// NewManager creates a new lock manager.
func NewManager(agentsDir string) *Manager {
	return &Manager{
		path: filepath.Join(agentsDir, "locks.json"),
	}
}

// Load reads the lock file from disk.
func (m *Manager) Load() (*LockFile, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LockFile{}, nil
		}
		return nil, fmt.Errorf("read locks: %w", err)
	}

	var lf LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse locks: %w", err)
	}
	return &lf, nil
}

// Save writes the lock file to disk.
func (m *Manager) Save(lf *LockFile) error {
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal locks: %w", err)
	}
	return os.WriteFile(m.path, data, 0o644)
}

// Acquire claims file patterns for an agent. Returns error if any pattern conflicts.
func (m *Manager) Acquire(agentID string, patterns []string) error {
	lf, err := m.Load()
	if err != nil {
		return err
	}

	// Check for conflicts
	for _, pattern := range patterns {
		for _, lock := range lf.Locks {
			if lock.AgentID == agentID {
				continue
			}
			if patternsOverlap(pattern, lock.Pattern) {
				return fmt.Errorf("conflict: %q overlaps with %q owned by agent %q", pattern, lock.Pattern, lock.AgentID)
			}
		}
	}

	// Add locks
	now := time.Now()
	for _, pattern := range patterns {
		lf.Locks = append(lf.Locks, FileLock{
			Pattern:  pattern,
			AgentID:  agentID,
			LockedAt: now,
		})
	}

	return m.Save(lf)
}

// Release removes all locks for an agent.
func (m *Manager) Release(agentID string) error {
	lf, err := m.Load()
	if err != nil {
		return err
	}

	var remaining []FileLock
	for _, lock := range lf.Locks {
		if lock.AgentID != agentID {
			remaining = append(remaining, lock)
		}
	}
	lf.Locks = remaining

	return m.Save(lf)
}

// Check returns the owner of a file path, if locked.
func (m *Manager) Check(path string) (ownerAgentID string, locked bool) {
	lf, err := m.Load()
	if err != nil {
		return "", false
	}

	for _, lock := range lf.Locks {
		matched, _ := filepath.Match(lock.Pattern, path)
		if matched {
			return lock.AgentID, true
		}
		// Also check if the path is under a directory pattern (e.g., "dir/**")
		if matchGlobDir(lock.Pattern, path) {
			return lock.AgentID, true
		}
	}
	return "", false
}

// HasConflict checks if any patterns conflict with existing locks from other agents.
func (m *Manager) HasConflict(agentID string, patterns []string) ([]FileLock, bool) {
	lf, err := m.Load()
	if err != nil {
		return nil, false
	}

	var conflicts []FileLock
	for _, pattern := range patterns {
		for _, lock := range lf.Locks {
			if lock.AgentID == agentID {
				continue
			}
			if patternsOverlap(pattern, lock.Pattern) {
				conflicts = append(conflicts, lock)
			}
		}
	}
	return conflicts, len(conflicts) > 0
}

// ListForAgent returns all locks held by an agent.
func (m *Manager) ListForAgent(agentID string) []FileLock {
	lf, err := m.Load()
	if err != nil {
		return nil
	}

	var locks []FileLock
	for _, lock := range lf.Locks {
		if lock.AgentID == agentID {
			locks = append(locks, lock)
		}
	}
	return locks
}

// patternsOverlap checks if two glob patterns might match the same files.
// Conservative: if either pattern is a prefix of the other or they share a common directory, assume overlap.
func patternsOverlap(a, b string) bool {
	// Exact match
	if a == b {
		return true
	}

	// Check if one is a subdirectory of the other
	dirA := filepath.Dir(a)
	dirB := filepath.Dir(b)

	// If patterns share the same directory, they might overlap
	if dirA == dirB {
		return true
	}

	// Check prefix relationships (e.g., "src/**" overlaps with "src/foo/bar.go")
	if matchGlobDir(a, b) || matchGlobDir(b, a) {
		return true
	}

	return false
}

// matchGlobDir checks if a path falls under a directory glob pattern.
// e.g., "internal/auth/**" matches "internal/auth/jwt.go"
func matchGlobDir(pattern, path string) bool {
	// Handle "dir/**" patterns
	if len(pattern) > 3 && pattern[len(pattern)-3:] == "/**" {
		dir := pattern[:len(pattern)-3]
		if len(path) > len(dir) && path[:len(dir)] == dir {
			return true
		}
	}
	return false
}
