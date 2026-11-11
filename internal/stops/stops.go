// Package stops is the short list of what a thread stops and asks Ashvin
// for, and the log of every question the ask hook let through or sent back.
package stops

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed default.md
var defaultList string

// Stop is one kind of question that needs Ashvin.
type Stop struct {
	Kind string
	What string
}

var line = regexp.MustCompile(`^- ([A-Za-z][A-Za-z ]{0,30}): (.+)$`)

// Path is the list Ashvin edits.
func Path(oxHome string) string { return filepath.Join(oxHome, "stops.md") }

// Parse reads the "- Kind: what" lines of a list.
func Parse(text string) []Stop {
	var out []Stop
	for l := range strings.SplitSeq(text, "\n") {
		if m := line.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			out = append(out, Stop{Kind: m[1], What: m[2]})
		}
	}
	return out
}

// Load reads the list, or the default when the file is missing or names no
// stop, so a broken edit never lets every question through.
func Load(oxHome string) (list []Stop, text string) {
	if data, err := os.ReadFile(Path(oxHome)); err == nil {
		if list := Parse(string(data)); len(list) > 0 {
			return list, string(data)
		}
	}
	return Parse(defaultList), defaultList
}

// Install writes the default list unless one is there. It reports whether it
// wrote it.
func Install(oxHome string) (bool, error) {
	path := Path(oxHome)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(oxHome, 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(defaultList), 0o644)
}

// KindOf is the stop a question starts with ("Send: post…" is Send), or "".
func KindOf(question string, list []Stop) string {
	head, _, ok := strings.Cut(strings.TrimSpace(question), ":")
	if !ok {
		return ""
	}
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(head), s.Kind) {
			return s.Kind
		}
	}
	return ""
}

// Entry is one question the ask hook saw.
type Entry struct {
	At       time.Time `json:"at"`
	Session  string    `json:"session"` // Claude Code's session id
	Kind     string    `json:"kind,omitempty"`
	Question string    `json:"question"`
	Passed   bool      `json:"passed"`
}

func logPath(oxHome string) string { return filepath.Join(oxHome, "logs", "stops.jsonl") }

// Record appends an entry to the log.
func Record(oxHome string, e Entry) error {
	path := logPath(oxHome)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(e)
}

// Since reads the entries at or after t, oldest first. Lines it can't read
// are skipped.
func Since(oxHome string, t time.Time) []Entry {
	data, err := os.ReadFile(logPath(oxHome))
	if err != nil {
		return nil
	}
	var out []Entry
	for l := range strings.SplitSeq(string(data), "\n") {
		var e Entry
		if json.Unmarshal([]byte(l), &e) == nil && !e.At.Before(t) {
			out = append(out, e)
		}
	}
	return out
}
