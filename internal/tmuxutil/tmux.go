package tmuxutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
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
//
// Hardening (each guards a real observed failure mode in TUI targets):
//   - a stuck copy-mode pane silently swallows input → cancel it first
//   - large -l sends drop characters under PTY typeahead → paste via buffer
//   - Enter fired in the same burst as the text gets absorbed by the TUI's
//     paste detection → send it separately after a settle delay
func SendKeys(session, text string) error {
	exec.Command("tmux", "send-keys", "-t", session, "-X", "cancel").Run()

	if len(text) > 800 {
		if err := pasteViaBuffer(session, text); err != nil {
			return err
		}
	} else {
		cmd := exec.Command("tmux", "send-keys", "-t", session, "-l", text)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("tmux send-keys (text): %w\n%s", err, output)
		}
	}

	time.Sleep(300 * time.Millisecond)
	cmd := exec.Command("tmux", "send-keys", "-t", session, "Enter")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys (enter): %w\n%s", err, output)
	}
	return nil
}

// pasteViaBuffer delivers large text through tmux's paste buffer (bracketed
// paste), which survives sizes that character-by-character send-keys drops.
func pasteViaBuffer(session, text string) error {
	f, err := os.CreateTemp("", "ox-paste-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	f.Close()

	if output, err := exec.Command("tmux", "load-buffer", "-b", "oxpaste", f.Name()).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w\n%s", err, output)
	}
	if output, err := exec.Command("tmux", "paste-buffer", "-p", "-d", "-b", "oxpaste", "-t", session).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux paste-buffer: %w\n%s", err, output)
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

// RenameWindow renames a window (target: "session:index").
func RenameWindow(target, name string) error {
	output, err := exec.Command("tmux", "rename-window", "-t", target, name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux rename-window: %w\n%s", err, output)
	}
	return nil
}

// NewWindow adds a window to an existing session and runs an optional command.
func NewWindow(session, name, dir, command string) error {
	args := []string{"new-window", "-d", "-t", session, "-n", name}
	if dir != "" {
		args = append(args, "-c", dir)
	}
	if command != "" {
		args = append(args, command)
	}
	output, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-window: %w\n%s", err, output)
	}
	return nil
}

// HasWindow reports whether a named window exists in a session.
func HasWindow(session, name string) bool {
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// RespawnWindow kills whatever runs in a window and starts a new command there.
func RespawnWindow(target, command string) error {
	output, err := exec.Command("tmux", "respawn-window", "-k", "-t", target, command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux respawn-window: %w\n%s", err, output)
	}
	return nil
}

// InsideTmux reports whether the current process runs inside a tmux client.
func InsideTmux() bool {
	return os.Getenv("TMUX") != ""
}

// SwitchClient switches the attached tmux client to another session.
func SwitchClient(name string) error {
	output, err := exec.Command("tmux", "switch-client", "-t", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux switch-client: %w\n%s", err, output)
	}
	return nil
}
