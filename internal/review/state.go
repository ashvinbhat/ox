package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReviewRecord is one completed review run against a PR. Records survive
// across worktree rebuilds and feed the follow-up review's addressing
// analysis on subsequent runs.
type ReviewRecord struct {
	ReviewedAt time.Time `json:"reviewedAt"`
	HeadSHA    string    `json:"headSHA"`
	ReviewID   int64     `json:"reviewID,omitempty"`  // 0 = not posted (self-PR, --no-interactive, user declined)
	ReviewURL  string    `json:"reviewURL,omitempty"` // GitHub html_url; empty for unposted reviews
	Posted     bool      `json:"posted"`              // true iff the review hit GitHub
	Findings   []Finding `json:"findings"`            // all findings the agents produced (with Ref + CommentID stamped if posted)
}

// State is the persisted history of reviews for a single PR.
type State struct {
	PR        int            `json:"pr"`
	OwnerRepo string         `json:"ownerRepo"`
	History   []ReviewRecord `json:"history"`
}

// LastReview returns the most recent record, or nil if none exists yet.
func (s *State) LastReview() *ReviewRecord {
	if s == nil || len(s.History) == 0 {
		return nil
	}
	return &s.History[len(s.History)-1]
}

// StatePath returns the on-disk location for a given PR's review state.
// Layout: ~/.ox/reviews/<owner>-<repo>-<pr>.json
func StatePath(oxHome, ownerRepo string, pr int) string {
	// Replace '/' with '-' so we get a flat filename.
	safe := strings.ReplaceAll(ownerRepo, "/", "-")
	return filepath.Join(oxHome, "reviews", fmt.Sprintf("%s-%d.json", safe, pr))
}

// LoadState reads the on-disk review state for a PR. Returns (nil, nil) if
// no state exists (first review). Returns an error only for malformed
// state files or unreadable paths.
func LoadState(oxHome, ownerRepo string, pr int) (*State, error) {
	path := StatePath(oxHome, ownerRepo, pr)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	return &s, nil
}

// SaveState writes the state atomically. Assigns Ref labels (F1, F2, ...)
// to any findings in the latest record that don't yet have one, so future
// runs can reference them by stable name.
func SaveState(oxHome string, s *State) error {
	if s == nil {
		return fmt.Errorf("save nil state")
	}
	// Stamp refs on the most recent record (others have already been stamped).
	if last := s.LastReview(); last != nil {
		for i := range last.Findings {
			if last.Findings[i].Ref == "" {
				last.Findings[i].Ref = fmt.Sprintf("F%d", i+1)
			}
		}
	}

	path := StatePath(oxHome, s.OwnerRepo, s.PR)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create reviews dir: %w", err)
	}

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	// Atomic write: temp + rename so a concurrent read can't observe a partial file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write tmp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}

// AppendRecord adds a new review record to the state (in-memory). The caller
// must SaveState afterward to persist.
func (s *State) AppendRecord(rec ReviewRecord) {
	s.History = append(s.History, rec)
}
