// Package checkpoint manages progress checkpoints for task workspaces.
package checkpoint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Checkpoint represents a saved progress point.
type Checkpoint struct {
	ID           string    `yaml:"id"`
	TaskID       string    `yaml:"task_id"`
	CreatedAt    time.Time `yaml:"created_at"`
	Done         string    `yaml:"done,omitempty"`          // What was completed
	Next         string    `yaml:"next,omitempty"`          // What's next
	FilesChanged []string  `yaml:"files_changed,omitempty"` // Files modified
	Decisions    []string  `yaml:"decisions,omitempty"`     // Key decisions made
	Blockers     []string  `yaml:"blockers,omitempty"`      // Current blockers
	Persona      string    `yaml:"persona,omitempty"`       // Active persona
}

// Manager manages checkpoints for a workspace.
type Manager struct {
	workspacePath string
	checkpointDir string
	taskID        string
}

// NewManager creates a checkpoint manager for a workspace.
func NewManager(workspacePath, taskID string) *Manager {
	return &Manager{
		workspacePath: workspacePath,
		checkpointDir: filepath.Join(workspacePath, ".checkpoints"),
		taskID:        taskID,
	}
}

// Create creates a new checkpoint.
func (m *Manager) Create(done, next string, decisions []string) (*Checkpoint, error) {
	if err := os.MkdirAll(m.checkpointDir, 0o755); err != nil {
		return nil, fmt.Errorf("create checkpoint dir: %w", err)
	}

	// Generate checkpoint ID
	id := time.Now().Format("20060102-150405")

	// Get files changed since last checkpoint
	filesChanged := m.getChangedFiles()

	cp := &Checkpoint{
		ID:           id,
		TaskID:       m.taskID,
		CreatedAt:    time.Now(),
		Done:         done,
		Next:         next,
		FilesChanged: filesChanged,
		Decisions:    decisions,
	}

	// Save checkpoint
	path := filepath.Join(m.checkpointDir, id+".yaml")
	data, err := yaml.Marshal(cp)
	if err != nil {
		return nil, fmt.Errorf("marshal checkpoint: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, fmt.Errorf("write checkpoint: %w", err)
	}

	return cp, nil
}

// List returns all checkpoints for the workspace, sorted by time (newest first).
func (m *Manager) List() ([]*Checkpoint, error) {
	entries, err := os.ReadDir(m.checkpointDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoint dir: %w", err)
	}

	var checkpoints []*Checkpoint
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}

		cp, err := m.load(filepath.Join(m.checkpointDir, e.Name()))
		if err != nil {
			continue
		}
		checkpoints = append(checkpoints, cp)
	}

	// Sort by created_at descending (newest first)
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].CreatedAt.After(checkpoints[j].CreatedAt)
	})

	return checkpoints, nil
}

// Latest returns the most recent checkpoint.
func (m *Manager) Latest() (*Checkpoint, error) {
	checkpoints, err := m.List()
	if err != nil {
		return nil, err
	}
	if len(checkpoints) == 0 {
		return nil, nil
	}
	return checkpoints[0], nil
}

// Get returns a checkpoint by ID.
func (m *Manager) Get(id string) (*Checkpoint, error) {
	path := filepath.Join(m.checkpointDir, id+".yaml")
	return m.load(path)
}

// load reads a checkpoint from disk.
func (m *Manager) load(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cp Checkpoint
	if err := yaml.Unmarshal(data, &cp); err != nil {
		return nil, err
	}

	return &cp, nil
}

// getChangedFiles returns files changed since the last checkpoint.
func (m *Manager) getChangedFiles() []string {
	var allFiles []string

	// Find repo directories (symlinks in workspace)
	entries, err := os.ReadDir(m.workspacePath)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		// Skip non-symlinks and special files
		if e.Name() == ".checkpoints" || e.Name() == "AGENTS.md" || e.Name() == "CLAUDE.md" {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		// Check if it's a symlink (repo directory)
		if info.Mode()&os.ModeSymlink != 0 {
			repoPath := filepath.Join(m.workspacePath, e.Name())
			target, err := os.Readlink(repoPath)
			if err != nil {
				continue
			}

			// Get changed files in this repo
			files := m.getRepoChangedFiles(target, e.Name())
			allFiles = append(allFiles, files...)
		}
	}

	return allFiles
}

// getRepoChangedFiles returns changed files in a git repo.
func (m *Manager) getRepoChangedFiles(repoPath, repoName string) []string {
	var files []string

	// Get staged and unstaged changes
	cmd := exec.Command("git", "diff", "--name-only", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// Try without HEAD (for repos with no commits on branch yet)
		cmd = exec.Command("git", "diff", "--name-only")
		cmd.Dir = repoPath
		output, _ = cmd.Output()
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			files = append(files, filepath.Join(repoName, line))
		}
	}

	// Also get untracked files
	cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = repoPath
	output, err = cmd.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if line != "" {
				files = append(files, filepath.Join(repoName, line))
			}
		}
	}

	return files
}

// ToMarkdown formats a checkpoint as markdown for display or notes.
func (cp *Checkpoint) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Checkpoint %s\n", cp.ID))
	sb.WriteString(fmt.Sprintf("*%s*\n\n", cp.CreatedAt.Format("2006-01-02 15:04")))

	if cp.Done != "" {
		sb.WriteString(fmt.Sprintf("**Done:** %s\n\n", cp.Done))
	}

	if cp.Next != "" {
		sb.WriteString(fmt.Sprintf("**Next:** %s\n\n", cp.Next))
	}

	if len(cp.FilesChanged) > 0 {
		sb.WriteString("**Files Changed:**\n")
		for _, f := range cp.FilesChanged {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
		sb.WriteString("\n")
	}

	if len(cp.Decisions) > 0 {
		sb.WriteString("**Decisions:**\n")
		for _, d := range cp.Decisions {
			sb.WriteString(fmt.Sprintf("- %s\n", d))
		}
		sb.WriteString("\n")
	}

	if len(cp.Blockers) > 0 {
		sb.WriteString("**Blockers:**\n")
		for _, b := range cp.Blockers {
			sb.WriteString(fmt.Sprintf("- %s\n", b))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ToYokeNote formats a checkpoint for syncing to yoke.
func (cp *Checkpoint) ToYokeNote() string {
	var parts []string

	if cp.Done != "" {
		parts = append(parts, fmt.Sprintf("Done: %s", cp.Done))
	}
	if cp.Next != "" {
		parts = append(parts, fmt.Sprintf("Next: %s", cp.Next))
	}
	if len(cp.FilesChanged) > 0 {
		parts = append(parts, fmt.Sprintf("Files: %d changed", len(cp.FilesChanged)))
	}

	return fmt.Sprintf("[checkpoint %s] %s", cp.ID, strings.Join(parts, " | "))
}
