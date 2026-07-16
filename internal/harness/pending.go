package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ashvinbhat/ox/internal/mission"
)

// PendingOnUser reports what a mission is waiting on the human for — the
// cross-mission attention signal. Priority: unanswered blocker, then plan
// review, then ship approval, then a parked PR. Empty when nothing needs
// the user.
func PendingOnUser(m *mission.Mission) string {
	if !m.Open() {
		return ""
	}

	if q, ok := openBlocker(m); ok {
		return "blocked: " + q
	}
	switch m.Phase {
	case "planning":
		if _, err := os.Stat(filepath.Join(m.Dir(), "plan.md")); err == nil {
			return fmt.Sprintf("plan awaiting review (ox plan %s)", m.ID)
		}
	case "reviewing":
		return "ship approval pending"
	case "shipping":
		if len(m.PRs) > 0 {
			return fmt.Sprintf("PR awaiting merge (%d open)", len(m.PRs))
		}
	}
	return ""
}

// openBlocker finds a recent agent_blocker whose worker hasn't finished
// since — the closest signal we have for "still waiting on an answer".
func openBlocker(m *mission.Mission) (string, bool) {
	events, err := m.EventsSince(0)
	if err != nil {
		return "", false
	}
	doneAfter := map[string]bool{}
	var q string
	var at time.Time
	var blockedID string
	for _, ev := range events {
		id, _ := ev.Data["id"].(string)
		switch ev.Type {
		case "agent_blocker":
			if question, _ := ev.Data["question"].(string); question != "" {
				q, blockedID, at = question, id, ev.TS
				doneAfter[id] = false
			}
		case "agent_done", "agent_killed":
			doneAfter[id] = true
		}
	}
	if q == "" || blockedID == "" || doneAfter[blockedID] {
		return "", false
	}
	if time.Since(at) > 24*time.Hour {
		return "", false
	}
	return q, true
}
