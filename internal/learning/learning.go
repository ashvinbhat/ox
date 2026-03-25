// Package learning manages insights and learnings across tasks.
package learning

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Category represents types of learnings.
type Category string

const (
	CategoryApproach Category = "approach" // Approaches that worked
	CategoryGotcha   Category = "gotcha"   // Gotchas discovered
	CategoryTool     Category = "tool"     // Tool preferences
	CategoryPattern  Category = "pattern"  // Patterns observed
	CategoryGeneral  Category = "general"  // General insights
)

// Learning represents a captured insight.
type Learning struct {
	ID        int64     `json:"id"`
	Content   string    `json:"content"`
	Category  Category  `json:"category"`
	Tags      []string  `json:"tags"`
	TaskSeq   *int      `json:"task_seq,omitempty"` // Optional link to task
	CreatedAt time.Time `json:"created_at"`
}

// Store manages learnings in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a learning store.
func NewStore(oxHome string) (*Store, error) {
	dbPath := filepath.Join(oxHome, "learnings.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the store.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates tables if they don't exist.
func migrate(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS learnings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content TEXT NOT NULL,
		category TEXT NOT NULL DEFAULT 'general',
		task_seq INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS learning_tags (
		learning_id INTEGER NOT NULL,
		tag TEXT NOT NULL,
		PRIMARY KEY (learning_id, tag),
		FOREIGN KEY (learning_id) REFERENCES learnings(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_learning_tags_tag ON learning_tags(tag);
	CREATE INDEX IF NOT EXISTS idx_learnings_category ON learnings(category);
	`
	_, err := db.Exec(schema)
	return err
}

// Add creates a new learning.
func (s *Store) Add(content string, category Category, tags []string, taskSeq *int) (*Learning, error) {
	if category == "" {
		category = CategoryGeneral
	}

	var result sql.Result
	var err error

	if taskSeq != nil {
		result, err = s.db.Exec(
			"INSERT INTO learnings (content, category, task_seq) VALUES (?, ?, ?)",
			content, category, *taskSeq,
		)
	} else {
		result, err = s.db.Exec(
			"INSERT INTO learnings (content, category) VALUES (?, ?)",
			content, category,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("insert learning: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get id: %w", err)
	}

	// Insert tags
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			_, err := s.db.Exec(
				"INSERT OR IGNORE INTO learning_tags (learning_id, tag) VALUES (?, ?)",
				id, tag,
			)
			if err != nil {
				return nil, fmt.Errorf("insert tag: %w", err)
			}
		}
	}

	return &Learning{
		ID:        id,
		Content:   content,
		Category:  category,
		Tags:      tags,
		TaskSeq:   taskSeq,
		CreatedAt: time.Now(),
	}, nil
}

// List returns all learnings, optionally filtered.
type ListOptions struct {
	Category Category
	Tag      string
	Limit    int
}

// List returns learnings matching the options.
func (s *Store) List(opts ListOptions) ([]*Learning, error) {
	query := `
		SELECT DISTINCT l.id, l.content, l.category, l.task_seq, l.created_at
		FROM learnings l
		LEFT JOIN learning_tags lt ON l.id = lt.learning_id
		WHERE 1=1
	`
	var args []interface{}

	if opts.Category != "" {
		query += " AND l.category = ?"
		args = append(args, opts.Category)
	}

	if opts.Tag != "" {
		query += " AND lt.tag = ?"
		args = append(args, strings.ToLower(opts.Tag))
	}

	query += " ORDER BY l.created_at DESC"

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query learnings: %w", err)
	}
	defer rows.Close()

	var learnings []*Learning
	for rows.Next() {
		l := &Learning{}
		var taskSeq sql.NullInt64
		if err := rows.Scan(&l.ID, &l.Content, &l.Category, &taskSeq, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan learning: %w", err)
		}
		if taskSeq.Valid {
			seq := int(taskSeq.Int64)
			l.TaskSeq = &seq
		}

		// Get tags for this learning
		tags, err := s.getTags(l.ID)
		if err != nil {
			return nil, err
		}
		l.Tags = tags

		learnings = append(learnings, l)
	}

	return learnings, nil
}

// SearchByTags returns learnings matching any of the given tags.
func (s *Store) SearchByTags(tags []string, limit int) ([]*Learning, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	// Build placeholders
	placeholders := make([]string, len(tags))
	args := make([]interface{}, len(tags))
	for i, tag := range tags {
		placeholders[i] = "?"
		args[i] = strings.ToLower(tag)
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT l.id, l.content, l.category, l.task_seq, l.created_at
		FROM learnings l
		JOIN learning_tags lt ON l.id = lt.learning_id
		WHERE lt.tag IN (%s)
		ORDER BY l.created_at DESC
	`, strings.Join(placeholders, ","))

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search learnings: %w", err)
	}
	defer rows.Close()

	var learnings []*Learning
	for rows.Next() {
		l := &Learning{}
		var taskSeq sql.NullInt64
		if err := rows.Scan(&l.ID, &l.Content, &l.Category, &taskSeq, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan learning: %w", err)
		}
		if taskSeq.Valid {
			seq := int(taskSeq.Int64)
			l.TaskSeq = &seq
		}

		tags, err := s.getTags(l.ID)
		if err != nil {
			return nil, err
		}
		l.Tags = tags

		learnings = append(learnings, l)
	}

	return learnings, nil
}

// getTags returns tags for a learning.
func (s *Store) getTags(learningID int64) ([]string, error) {
	rows, err := s.db.Query("SELECT tag FROM learning_tags WHERE learning_id = ?", learningID)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// Delete removes a learning by ID.
func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM learnings WHERE id = ?", id)
	return err
}

// GetAllTags returns all unique tags.
func (s *Store) GetAllTags() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT tag FROM learning_tags ORDER BY tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// Count returns the total number of learnings.
func (s *Store) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM learnings").Scan(&count)
	return count, err
}
