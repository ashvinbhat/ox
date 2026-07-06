package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MigrateLearnings imports rows from the legacy learnings.db. Legacy tags are
// mostly repo names (ox done --learn passed workspace repos as tags), which is
// how rows land in repo scopes. Idempotent: rows already migrated (matched by
// source marker) are skipped.
func (s *Store) MigrateLearnings(ctx context.Context, oxHome string, repoNames map[string]bool) (int, int, error) {
	legacyPath := filepath.Join(oxHome, "learnings.db")
	if _, err := os.Stat(legacyPath); err != nil {
		return 0, 0, fmt.Errorf("no learnings.db at %s", legacyPath)
	}

	legacy, err := sql.Open("sqlite", legacyPath)
	if err != nil {
		return 0, 0, err
	}
	defer legacy.Close()

	rows, err := legacy.Query(`
SELECT l.id, l.content, l.category, l.task_seq,
       COALESCE((SELECT GROUP_CONCAT(t.tag) FROM learning_tags t WHERE t.learning_id = l.id), '')
FROM learnings l ORDER BY l.id`)
	if err != nil {
		return 0, 0, fmt.Errorf("read learnings: %w", err)
	}
	defer rows.Close()

	kindMap := map[string]string{
		"approach": "learning",
		"gotcha":   "gotcha",
		"tool":     "tool",
		"pattern":  "convention",
		"general":  "learning",
	}

	migrated, skipped := 0, 0
	for rows.Next() {
		var id int64
		var content, category, tagsCSV string
		var taskSeq sql.NullInt64
		if err := rows.Scan(&id, &content, &category, &taskSeq, &tagsCSV); err != nil {
			continue
		}

		source := fmt.Sprintf("migration:learnings#%d", id)
		var exists int
		s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE source=?`, source).Scan(&exists)
		if exists > 0 {
			skipped++
			continue
		}

		var tags []string
		scope := "global"
		for _, t := range splitCSV(tagsCSV) {
			tags = append(tags, t)
			if scope == "global" && repoNames[t] {
				scope = "repo:" + t
			}
		}
		if scope == "global" && taskSeq.Valid {
			scope = fmt.Sprintf("task:%d", taskSeq.Int64)
		}

		kind := kindMap[category]
		if kind == "" {
			kind = "learning"
		}

		if _, err := s.insertRaw(RememberInput{
			Content: content,
			Kind:    kind,
			Scope:   scope,
			Tags:    tags,
			Source:  source,
		}); err != nil {
			return migrated, skipped, err
		}
		migrated++
	}
	return migrated, skipped, nil
}

// insertRaw bypasses the dedupe gate (migration wants faithful copies) and
// leaves embeddings NULL for the backfill pass.
func (s *Store) insertRaw(in RememberInput) (string, error) {
	if in.Title == "" {
		in.Title = firstSentence(in.Content)
	}
	return s.insert(in, nil)
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
