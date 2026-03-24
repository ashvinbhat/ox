package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TaskWorkspace represents an active task workspace.
type TaskWorkspace struct {
	Path     string   // absolute path to task directory
	TaskID   string   // yoke task ID
	TaskSeq  int      // yoke task sequence number
	Slug     string   // directory name (e.g., 9-refactor-auth)
	Repos    []string // repos included in this workspace
	Persona  string   // active persona
}

// Create creates a new task workspace directory under oxHome/tasks/.
func Create(oxHome string, taskID string, taskSeq int, title string) (*TaskWorkspace, error) {
	slug := makeSlug(taskSeq, title)
	dir := filepath.Join(oxHome, "tasks", slug)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create task dir: %w", err)
	}

	return &TaskWorkspace{
		Path:    dir,
		TaskID:  taskID,
		TaskSeq: taskSeq,
		Slug:    slug,
	}, nil
}

// Open finds an existing task workspace by task ID or seq.
func Open(oxHome string, taskRef string) (*TaskWorkspace, error) {
	tasksDir := filepath.Join(oxHome, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no task workspace for %s", taskRef)
		}
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Match by seq prefix (e.g., "9-" matches "9-refactor-auth")
		if strings.HasPrefix(name, taskRef+"-") || name == taskRef {
			ws := &TaskWorkspace{
				Path: filepath.Join(tasksDir, name),
				Slug: name,
			}
			ws.TaskSeq = extractSeq(name)
			ws.loadState()
			return ws, nil
		}
	}

	return nil, fmt.Errorf("no task workspace for %s", taskRef)
}

// Remove deletes a task workspace directory.
func Remove(oxHome string, taskRef string) error {
	ws, err := Open(oxHome, taskRef)
	if err != nil {
		return err
	}
	return os.RemoveAll(ws.Path)
}

// List returns all task workspaces.
func List(oxHome string) ([]*TaskWorkspace, error) {
	tasksDir := filepath.Join(oxHome, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}

	var workspaces []*TaskWorkspace
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ws := &TaskWorkspace{
			Path:    filepath.Join(tasksDir, e.Name()),
			Slug:    e.Name(),
			TaskSeq: extractSeq(e.Name()),
		}
		ws.loadState()
		workspaces = append(workspaces, ws)
	}
	return workspaces, nil
}

// AddRepoLink creates a symlink to a worktree in the workspace.
func (ws *TaskWorkspace) AddRepoLink(repoName, worktreePath string) error {
	linkPath := filepath.Join(ws.Path, repoName)

	// Remove existing link if present
	os.Remove(linkPath)

	if err := os.Symlink(worktreePath, linkPath); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	ws.Repos = append(ws.Repos, repoName)
	return nil
}

// loadState loads workspace state by examining the workspace directory.
func (ws *TaskWorkspace) loadState() {
	// Discover repos by finding symlinks in the workspace
	entries, err := os.ReadDir(ws.Path)
	if err != nil {
		return
	}

	// Known non-repo symlinks to exclude
	exclude := map[string]bool{
		"CLAUDE.md": true,
		"AGENTS.md": true,
	}

	for _, e := range entries {
		// Skip known non-repo files
		if exclude[e.Name()] {
			continue
		}

		// Check if it's a symlink
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			ws.Repos = append(ws.Repos, e.Name())
		}
	}
}

// SaveState saves workspace state to state.yaml.
func (ws *TaskWorkspace) SaveState() error {
	// TODO: Save to state.yaml
	return nil
}

// makeSlug creates a filesystem-safe directory name from seq and title.
func makeSlug(seq int, title string) string {
	// Convert title to slug
	slug := strings.ToLower(title)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	// Limit length
	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.TrimRight(slug, "-")
	}

	return fmt.Sprintf("%d-%s", seq, slug)
}

// extractSeq extracts the sequence number from a workspace slug.
func extractSeq(slug string) int {
	var seq int
	fmt.Sscanf(slug, "%d-", &seq)
	return seq
}
