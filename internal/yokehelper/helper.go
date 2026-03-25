// Package yokehelper provides convenient access to yoke's task store.
package yokehelper

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ashvinbhat/yoke/task"
)

// Client wraps yoke's task store with convenient methods.
type Client struct {
	store *task.Store
}

// NewClient creates a new yoke client.
func NewClient() (*Client, error) {
	dbPath, err := findYokeDB()
	if err != nil {
		return nil, err
	}

	store, err := task.NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open yoke store: %w", err)
	}

	return &Client{store: store}, nil
}

// Close closes the store.
func (c *Client) Close() error {
	return c.store.Close()
}

// Store returns the underlying task store for direct access.
func (c *Client) Store() *task.Store {
	return c.store
}

// Get retrieves a task by ID or seq number.
func (c *Client) Get(ref string) (*task.Task, error) {
	return c.store.Get(ref)
}

// GetNotes retrieves all notes for a task.
func (c *Client) GetNotes(taskID string) ([]task.Note, error) {
	return c.store.GetNotes(taskID)
}

// GetEvents retrieves events for a task.
func (c *Client) GetEvents(taskID string) ([]task.Event, error) {
	return c.store.GetEvents(taskID)
}

// List retrieves tasks with optional filtering.
func (c *Client) List(all bool, statusFilter string) ([]*task.Task, error) {
	opts := task.ListOptions{}

	if statusFilter != "" {
		s := task.Status(statusFilter)
		opts.Status = &s
	} else if !all {
		opts.Open = true
	}

	return c.store.List(opts)
}

// UpdateStatus updates a task's status.
func (c *Client) UpdateStatus(taskID string, newStatus string) error {
	t, err := c.store.GetByID(taskID)
	if err != nil {
		return err
	}

	return c.store.UpdateStatus(taskID, t.Status, task.Status(newStatus))
}

// UpdateOutcome updates a task's outcome field.
func (c *Client) UpdateOutcome(taskID string, outcome string) error {
	t, err := c.store.GetByID(taskID)
	if err != nil {
		return err
	}

	t.Outcome = &outcome
	return c.store.Update(t)
}

// GetParent retrieves the parent task if one exists.
func (c *Client) GetParent(t *task.Task) (*task.Task, error) {
	if t.Parent == nil || *t.Parent == "" {
		return nil, nil
	}
	return c.store.Get(*t.Parent)
}

// GetChildren retrieves all child tasks.
func (c *Client) GetChildren(taskID string) ([]*task.Task, error) {
	return c.store.List(task.ListOptions{ParentID: &taskID})
}

// GetBlockers retrieves tasks that block the given task.
func (c *Client) GetBlockers(t *task.Task) ([]*task.Task, error) {
	var blockers []*task.Task
	for _, blockerRef := range t.Blockers {
		blocker, err := c.store.Get(blockerRef)
		if err != nil {
			continue // Skip if blocker not found
		}
		blockers = append(blockers, blocker)
	}
	return blockers, nil
}

// AddNote adds a note to a task.
func (c *Client) AddNote(taskID string, content string) error {
	return c.store.AddNote(taskID, content)
}

// findYokeDB locates the yoke database file.
func findYokeDB() (string, error) {
	// Check YOKE_HOME env var
	if yokeHome := os.Getenv("YOKE_HOME"); yokeHome != "" {
		dbPath := filepath.Join(yokeHome, "yoke.db")
		if _, err := os.Stat(dbPath); err == nil {
			return dbPath, nil
		}
	}

	// Default to ~/.yoke/yoke.db
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	dbPath := filepath.Join(home, ".yoke", "yoke.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("yoke database not found (run 'yoke init')")
	}

	return dbPath, nil
}
