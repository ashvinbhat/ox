// Package park keeps the threads Ashvin left on purpose, each with the first
// step for when he comes back: ~/.ox/parked/<session>.json.
package park

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Slack covers the parking turn itself: the thread's last activity lands a
// moment after the record. Activity later than this means he came back.
const Slack = 10 * time.Minute

// Record is one parked thread.
type Record struct {
	SessionID string    `json:"sessionId"`
	Title     string    `json:"title"`
	Next      string    `json:"next"`
	At        time.Time `json:"at"`
}

// Holds reports whether a thread last active at lastActive is still parked.
func (r Record) Holds(lastActive time.Time) bool {
	return !lastActive.After(r.At.Add(Slack))
}

func Dir(oxHome string) string { return filepath.Join(oxHome, "parked") }

// Save writes the record, replacing any earlier one for the thread.
func Save(oxHome string, r Record) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(oxHome), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir(oxHome), r.SessionID+".json"), append(data, '\n'), 0o644)
}

// Load reads every record by session id. Unreadable files are skipped.
func Load(oxHome string) (map[string]Record, error) {
	entries, err := os.ReadDir(Dir(oxHome))
	if os.IsNotExist(err) {
		return map[string]Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]Record{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(oxHome), e.Name()))
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(data, &r) == nil && r.SessionID != "" {
			out[r.SessionID] = r
		}
	}
	return out, nil
}
