package harness

import (
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

// ClaudeAlive reports whether a claude process is (still) driving the pane —
// its UI footer is visible or it is mid-response.
func ClaudeAlive(target string) bool {
	out, err := tmuxutil.CapturePane(target, 15)
	if err != nil {
		return false
	}
	return strings.Contains(out, "bypass permissions") ||
		strings.Contains(out, "shift+tab to cycle") ||
		strings.Contains(out, "esc to interrupt")
}

// EnsureClaudeReady polls a tmux target until the Claude input prompt is up,
// answering the folder-trust dialog if it appears (fresh mission dirs and
// worktrees are never pre-trusted). Returns false on timeout.
func EnsureClaudeReady(target string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	trustAnswered := false

	for time.Now().Before(deadline) {
		out, err := tmuxutil.CapturePane(target, 25)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		if !trustAnswered && strings.Contains(out, "trust this folder") {
			tmuxutil.SendKeysRaw(target, "Enter")
			trustAnswered = true
			time.Sleep(2 * time.Second)
			continue
		}

		if strings.Contains(out, "bypass permissions") ||
			strings.Contains(out, "shift+tab to cycle") ||
			strings.Contains(out, "? for shortcuts") {
			return true
		}

		time.Sleep(time.Second)
	}
	return false
}
