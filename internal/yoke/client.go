// Package yoke provides read access to yoke's task database.
package yoke

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Task represents a yoke task (read-only view).
type Task struct {
	ID        string
	Seq       int
	Title     string
	Body      string
	Status    string
	Priority  int
	Tags      []string
	Parent    *string
	Blockers  []string
	NotionID  *string
	NotionURL *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Note represents a task note.
type Note struct {
	ID        int
	TaskID    string
	Content   string
	CreatedAt time.Time
}

// Event represents a task event.
type Event struct {
	ID        int
	TaskID    string
	EventType string
	OldValue  string
	NewValue  string
	CreatedAt time.Time
}

// Client provides read access to yoke's database.
type Client struct {
	db *sql.DB
}

// NewClient creates a new yoke client.
func NewClient() (*Client, error) {
	dbPath, err := findYokeDB()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	return &Client{db: db}, nil
}

// Close closes the database connection.
func (c *Client) Close() error {
	return c.db.Close()
}

// Get retrieves a task by ID or seq number.
func (c *Client) Get(ref string) (*Task, error) {
	query := `
		SELECT id, seq, title, body, status, priority, tags, parent_id, blockers,
		       notion_id, notion_url, created_at, updated_at
		FROM tasks
		WHERE id = ? OR seq = ? OR id LIKE ?
		LIMIT 1
	`

	var t Task
	var tagsJSON, blockersJSON string
	var parent, notionID, notionURL sql.NullString

	err := c.db.QueryRow(query, ref, ref, ref+"%").Scan(
		&t.ID, &t.Seq, &t.Title, &t.Body, &t.Status, &t.Priority,
		&tagsJSON, &parent, &blockersJSON, &notionID, &notionURL,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task not found: %s", ref)
		}
		return nil, fmt.Errorf("query task: %w", err)
	}

	// Parse JSON arrays
	if tagsJSON != "" && tagsJSON != "null" {
		json.Unmarshal([]byte(tagsJSON), &t.Tags)
	}
	if blockersJSON != "" && blockersJSON != "null" {
		json.Unmarshal([]byte(blockersJSON), &t.Blockers)
	}

	if parent.Valid {
		t.Parent = &parent.String
	}
	if notionID.Valid {
		t.NotionID = &notionID.String
	}
	if notionURL.Valid {
		t.NotionURL = &notionURL.String
	}

	return &t, nil
}

// GetNotes retrieves all notes for a task.
func (c *Client) GetNotes(taskID string) ([]Note, error) {
	query := `SELECT id, task_id, content, created_at FROM notes WHERE task_id = ? ORDER BY created_at`

	rows, err := c.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("query notes: %w", err)
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.TaskID, &n.Content, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		notes = append(notes, n)
	}

	return notes, nil
}

// GetEvents retrieves recent events for a task.
func (c *Client) GetEvents(taskID string) ([]Event, error) {
	query := `SELECT id, task_id, event_type, old_value, new_value, created_at
	          FROM events WHERE task_id = ? ORDER BY created_at DESC LIMIT 20`

	rows, err := c.db.Query(query, taskID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TaskID, &e.EventType, &e.OldValue, &e.NewValue, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}

	return events, nil
}

// UpdateStatus updates a task's status.
func (c *Client) UpdateStatus(taskID string, status string) error {
	_, err := c.db.Exec(`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), taskID)
	return err
}

// UpdateOutcome updates a task's outcome.
func (c *Client) UpdateOutcome(taskID string, outcome string) error {
	_, err := c.db.Exec(`UPDATE tasks SET outcome = ?, updated_at = ? WHERE id = ?`,
		outcome, time.Now(), taskID)
	return err
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
