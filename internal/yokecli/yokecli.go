// Package yokecli is ox's only bridge to yoke: it shells out to the yoke
// binary and decodes its --json output. The contract it depends on is
// yoke's documented CLI (see `yoke docs`), the same interface agents use
// directly — there is no compiled-in library link and no semantic layer.
package yokecli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Task mirrors yoke's `show --json` / `list --json` output contract.
type Task struct {
	ID          string     `json:"id"`
	Seq         int        `json:"seq"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	Priority    int        `json:"priority"`
	Tags        []string   `json:"tags"`
	ParentID    *string    `json:"parentId"`
	BlockerIDs  []string   `json:"blockerIds"`
	NotionURL   *string    `json:"notionUrl"`
	ExternalRef *string    `json:"externalRef"`
	LocalOnly   bool       `json:"localOnly"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	DoneAt      *time.Time `json:"doneAt"`
	Outcome     *string    `json:"outcome"`

	// Resolved relations — populated by `show --json` only.
	Parent    *TaskRef  `json:"parent"`
	Children  []TaskRef `json:"children"`
	BlockedBy []TaskRef `json:"blockedBy"`
}

type TaskRef struct {
	ID     string `json:"id"`
	Seq    int    `json:"seq"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type Note struct {
	TaskID    string    `json:"taskId"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// Status values are part of yoke's documented contract.
const (
	StatusPending    = "pending"
	StatusActive     = "active"
	StatusInProgress = "in_progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
	StatusDropped    = "dropped"
)

// BinaryPath resolves the yoke binary. Daemons and tmux sessions often run
// without ~/go/bin in PATH, so PATH lookup alone is not enough.
func BinaryPath() string {
	if path, err := exec.LookPath("yoke"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	for _, loc := range []string{
		filepath.Join(home, "go", "bin", "yoke"),
		"/usr/local/bin/yoke",
	} {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}
	return "yoke"
}

func run(args ...string) ([]byte, error) {
	cmd := exec.Command(BinaryPath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("yoke %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

func runJSON(v any, args ...string) error {
	out, err := run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("yoke %s: decode json: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Get fetches one task by seq or id, with resolved relations.
func Get(ref string) (*Task, error) {
	var t Task
	if err := runJSON(&t, "show", ref, "--json"); err != nil {
		return nil, err
	}
	return &t, nil
}

// List fetches tasks. all=false limits to open tasks; a non-empty status
// filters to exactly that status (and implies all).
func List(all bool, status string) ([]Task, error) {
	args := []string{"list", "--json"}
	if status != "" {
		args = append(args, "--status", status)
	} else if all {
		args = append(args, "--all")
	}
	var tasks []Task
	if err := runJSON(&tasks, args...); err != nil {
		return nil, err
	}
	return tasks, nil
}

// Notes fetches a task's notes.
func Notes(ref string) ([]Note, error) {
	var notes []Note
	if err := runJSON(&notes, "notes", ref, "--json"); err != nil {
		return nil, err
	}
	return notes, nil
}

// ContextMarkdown returns yoke's assembled task context as markdown —
// the canonical task section for agent-facing documents.
func ContextMarkdown(ref string) (string, error) {
	out, err := run("context", ref)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Start marks a task in_progress.
func Start(ref string) error {
	_, err := run("start", ref)
	return err
}

// Done marks a task done, optionally recording an outcome.
func Done(ref, outcome string) error {
	args := []string{"done", ref}
	if outcome != "" {
		args = append(args, "--outcome", outcome)
	}
	_, err := run(args...)
	return err
}

// AddNote appends a note to a task.
func AddNote(ref, text string) error {
	_, err := run("note", ref, text)
	return err
}

// DocsPath refreshes yoke's usage doc on disk and returns its path.
// Used to (re)point workspace symlinks at the canonical reference.
func DocsPath() (string, error) {
	out, err := run("docs")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TasksThisWeek returns tasks that are in progress, or were started or
// completed in the current ISO week. Used by the feedback reminder.
func TasksThisWeek() ([]Task, error) {
	all, err := List(true, "")
	if err != nil {
		return nil, err
	}

	now := time.Now()
	year, week := now.ISOWeek()
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, now.Location())
	weekday := jan4.Weekday()
	if weekday == 0 {
		weekday = 7
	}
	weekStart := jan4.AddDate(0, 0, (week-1)*7-int(weekday)+1)
	weekStart = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, now.Location())

	var result []Task
	for _, t := range all {
		switch {
		case t.Status == StatusInProgress:
			result = append(result, t)
		case t.Status == StatusDone && t.DoneAt != nil && !t.DoneAt.Before(weekStart):
			result = append(result, t)
		case t.StartedAt != nil && !t.StartedAt.Before(weekStart) && t.Status != StatusDropped:
			result = append(result, t)
		}
	}
	return result, nil
}
