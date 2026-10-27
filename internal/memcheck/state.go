package memcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Keep records Ashvin's call that a suggestion is fine as it stands (a name
// that lives outside the repos, a path that's meant to be gone). A kept
// suggestion never comes back.
type Keep struct {
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
}

// StateDir holds the last report and the keep list: ~/.ox/memory-check.
func StateDir(oxHome string) string { return filepath.Join(oxHome, "memory-check") }

func LoadKeep(oxHome string) (map[string]Keep, error) {
	keep := map[string]Keep{}
	data, err := os.ReadFile(filepath.Join(StateDir(oxHome), "keep.json"))
	if os.IsNotExist(err) {
		return keep, nil
	}
	if err != nil {
		return nil, err
	}
	return keep, json.Unmarshal(data, &keep)
}

func SaveKeep(oxHome string, keep map[string]Keep) error {
	return writeJSON(filepath.Join(StateDir(oxHome), "keep.json"), keep)
}

func SaveReport(oxHome string, r Report) error {
	return writeJSON(filepath.Join(StateDir(oxHome), "last.json"), r)
}

func LoadReport(oxHome string) (Report, error) {
	var r Report
	data, err := os.ReadFile(filepath.Join(StateDir(oxHome), "last.json"))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(data, &r)
}

// Pending is the last report minus anything kept since it ran.
func Pending(oxHome string) (Report, error) {
	r, err := LoadReport(oxHome)
	if err != nil {
		return r, err
	}
	keep, err := LoadKeep(oxHome)
	if err != nil {
		return r, err
	}
	open := r.Suggestions[:0]
	for _, s := range r.Suggestions {
		if _, ok := keep[s.ID]; ok {
			r.Kept++
			continue
		}
		open = append(open, s)
	}
	r.Suggestions = open
	return r, nil
}

// writeJSON replaces path in one rename, so a brief reading it while an
// audit writes never sees half a file.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
