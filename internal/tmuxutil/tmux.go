package tmuxutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// NewSession creates a new detached tmux session with the given name and working directory.
func NewSession(name, dir string) error {
	args := []string{"new-session", "-d", "-s", name}
	cmd := exec.Command("tmux", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session: %w\n%s", err, output)
	}
	return nil
}

// HasSession checks if a tmux session with the given name exists.
func HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// SendKeys sends text to a tmux session followed by Enter.
func SendKeys(session, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", session, "-l", text)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys (text): %w\n%s", err, output)
	}
	// Send Enter separately to avoid interpretation issues
	cmd = exec.Command("tmux", "send-keys", "-t", session, "Enter")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys (enter): %w\n%s", err, output)
	}
	return nil
}

// SendKeysRaw sends raw key names to a tmux session (e.g., "C-c", "Enter").
func SendKeysRaw(session string, keys ...string) error {
	args := append([]string{"send-keys", "-t", session}, keys...)
	cmd := exec.Command("tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys: %w\n%s", err, output)
	}
	return nil
}

// CapturePane captures the last N lines of a tmux session's pane.
func CapturePane(session string, lines int) (string, error) {
	startLine := fmt.Sprintf("-%d", lines)
	cmd := exec.Command("tmux", "capture-pane", "-t", session, "-p", "-S", startLine)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}
	return string(output), nil
}

// KillSession terminates a tmux session.
func KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-session: %w\n%s", err, output)
	}
	return nil
}

// SetEnv sets an environment variable in a tmux session.
func SetEnv(session, key, value string) error {
	cmd := exec.Command("tmux", "set-environment", "-t", session, key, value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux set-environment: %w\n%s", err, output)
	}
	return nil
}

// ListSessions lists tmux sessions matching a prefix. Returns session names.
func ListSessions(prefix string) ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		// No sessions is not an error
		if strings.Contains(err.Error(), "no server running") || strings.Contains(string(output), "no server") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %w", err)
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if prefix == "" || strings.HasPrefix(line, prefix) {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// Attach attaches the current terminal to a tmux session.
// This replaces the current process with tmux attach.
func Attach(name string) error {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}
	return syscall.Exec(tmuxPath, []string{"tmux", "attach", "-t", name}, os.Environ())
}

// IsAvailable checks if tmux is installed and available.
func IsAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}
