// Package desktop reads the Claude Code desktop app's per-thread state files so
// ox can show every local thread without a UI of its own. The format is
// internal to the app, so every field is optional and a file that no longer
// parses is skipped: a format change degrades `ox now`, it never breaks it.
package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// State is where a thread stands from the user's side.
type State string

const (
	StateNeedsYou State = "needs you"
	StateInReview State = "in review"
	StateWorking  State = "working"
	StateStuck    State = "stuck"
	StateDone     State = "done"
	// StateParked is ox's, not the app's: a thread left on purpose with a
	// next step (internal/park).
	StateParked State = "parked"
)

// States lists every state in the order ox shows them.
var States = []State{StateNeedsYou, StateInReview, StateWorking, StateStuck, StateParked, StateDone}

// A turn in flight has no post-turn summary yet; this much silence means the
// turn died rather than that it is still running.
const stuckAfter = 30 * time.Minute

// Done threads idle this long are worth archiving.
const archiveAfter = 48 * time.Hour

// PR is a pull request a thread opened.
type PR struct {
	Repo   string `json:"repo"`
	Number int    `json:"prNumber"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
	State  string `json:"state"`
}

// Summary is the app's one-line read of the thread's last turn.
type Summary struct {
	Status      string `json:"status_category"`
	Detail      string `json:"status_detail"`
	NeedsAction string `json:"needs_action"`
}

// WrapUp is the recap the thread wrote at the end of its last turn.
type WrapUp struct {
	Description string `json:"description"`
}

// Session is one desktop Code-tab thread.
type Session struct {
	ID           string          `json:"sessionId"`
	Title        string          `json:"title"`
	Cwd          string          `json:"cwd"`
	Archived     bool            `json:"isArchived"`
	LastActiveMs float64         `json:"lastActivityAt"`
	PRs          []PR            `json:"prs"`
	Summary      *Summary        `json:"postTurnSummary"`
	WrapUp       *WrapUp         `json:"turnWrapUp"`
	Interrupted  json.RawMessage `json:"interruptedAwaitingPerson"`
}

// DefaultDir is where the desktop app keeps thread state on macOS.
func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions")
}

// List returns the threads under dir, most recently active first. Archived
// threads are left out unless includeArchived.
func List(dir string, includeArchived bool) ([]Session, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "*", "local_*.json"))
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s Session
		if json.Unmarshal(b, &s) != nil || s.ID == "" {
			continue
		}
		if s.Archived && !includeArchived {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActiveMs > out[j].LastActiveMs })
	return out, nil
}

// Get reads one thread by its id (local_…), archived or not.
func Get(dir, id string) (Session, error) {
	if !strings.HasPrefix(id, "local_") || filepath.Base(id) != id {
		return Session{}, fmt.Errorf("not a thread id: %q", id)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*", "*", id+".json"))
	if err != nil {
		return Session{}, err
	}
	if len(paths) == 0 {
		return Session{}, fmt.Errorf("no thread %s", id)
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return Session{}, fmt.Errorf("read thread %s: %w", id, err)
	}
	return s, nil
}

// LastActive is when the thread last did anything.
func (s Session) LastActive() time.Time {
	return time.UnixMilli(int64(s.LastActiveMs))
}

// Ask is the question the thread is waiting on, if any.
func (s Session) Ask() string {
	if s.Summary == nil {
		return ""
	}
	return s.Summary.NeedsAction
}

// Recap is the thread's latest one-line status.
func (s Session) Recap() string {
	if s.Summary != nil && s.Summary.Detail != "" {
		return s.Summary.Detail
	}
	if s.WrapUp != nil {
		return s.WrapUp.Description
	}
	return ""
}

// OpenPRs are the thread's pull requests that are still open.
func (s Session) OpenPRs() []PR {
	var open []PR
	for _, p := range s.PRs {
		if p.State == "OPEN" {
			open = append(open, p)
		}
	}
	return open
}

// State classifies the thread as of now.
func (s Session) State(now time.Time) State {
	switch {
	case s.Ask() != "" || s.status() == "blocked" || isSet(s.Interrupted):
		return StateNeedsYou
	case s.status() == "":
		if now.Sub(s.LastActive()) < stuckAfter {
			return StateWorking
		}
		return StateStuck
	case s.status() == "review_ready" || len(s.OpenPRs()) > 0:
		return StateInReview
	default:
		return StateDone
	}
}

// Archivable reports a finished thread nobody has touched in two days.
func (s Session) Archivable(now time.Time) bool {
	return s.State(now) == StateDone && now.Sub(s.LastActive()) > archiveAfter
}

func (s Session) status() string {
	if s.Summary == nil {
		return ""
	}
	return s.Summary.Status
}

func isSet(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}
