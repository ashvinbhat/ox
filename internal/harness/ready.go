package harness

import (
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

// SendMessageEnsured types a message into a claude pane and makes sure it was
// actually submitted: the TUI sometimes swallows the Enter that follows a
// paste during renders, leaving the text stranded in the input box.
func SendMessageEnsured(target, msg string) error {
	if err := tmuxutil.SendKeys(target, msg); err != nil {
		return err
	}
	tail := msg
	if len(tail) > 30 {
		tail = msg[len(msg)-30:]
	}
	for range 3 {
		time.Sleep(2 * time.Second)
		out, err := tmuxutil.CapturePane(target, 20)
		if err != nil {
			return nil
		}
		if strings.Contains(out, "esc to interrupt") {
			return nil // submitted, response streaming
		}
		if !inputHolds(out, tail) {
			return nil
		}
		tmuxutil.SendKeysRaw(target, "Enter")
	}
	return nil
}

// inputHolds reports whether the prompt's input area still contains text.
func inputHolds(pane, tail string) bool {
	lines := strings.Split(strings.TrimRight(pane, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "─") ||
			strings.Contains(line, "bypass permissions") || strings.Contains(line, "for agents") ||
			strings.HasPrefix(line, "\\") {
			continue
		}
		if strings.HasPrefix(line, "❯") {
			content := strings.TrimSpace(strings.TrimPrefix(line, "❯"))
			return content != "" && (strings.Contains(pane, tail) || len(content) > 3)
		}
		return false
	}
	return false
}

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
