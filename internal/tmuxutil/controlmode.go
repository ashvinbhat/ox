package tmuxutil

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ControlStream attaches to a tmux session in control mode (-C) and streams
// decoded pane output. Unlike capture-pane polling, tmux pushes every byte
// the moment it renders, so viewers get real-time output.
type ControlStream struct {
	cmd    *exec.Cmd
	Events chan StreamEvent
	done   chan struct{}
}

type StreamEvent struct {
	PaneID string // "%3" — filter on this
	Data   string // decoded terminal bytes (ANSI intact)
	Exit   bool
}

// PaneID resolves the pane id for a target like "ox-m5:orc".
func PaneID(target string) (string, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", target, "-F", "#{pane_id}").Output()
	if err != nil {
		return "", fmt.Errorf("list-panes %s: %w", target, err)
	}
	id := strings.TrimSpace(strings.Split(strings.TrimSpace(string(out)), "\n")[0])
	if id == "" {
		return "", fmt.Errorf("no pane for %s", target)
	}
	return id, nil
}

// PaneSize returns cols, rows for a target.
func PaneSize(target string) (int, int) {
	out, err := exec.Command("tmux", "display", "-p", "-t", target, "#{pane_width} #{pane_height}").Output()
	if err != nil {
		return 200, 50
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return 200, 50
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w == 0 || h == 0 {
		return 200, 50
	}
	return w, h
}

// OpenControlStream attaches (read-only) to a session in control mode. The
// caller filters events by pane id and MUST call Close.
func OpenControlStream(session string) (*ControlStream, error) {
	// -r: read-only client — a viewer must never steal input focus or
	// count as an interactive attach.
	cmd := exec.Command("tmux", "-C", "attach-session", "-r", "-t", session)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("tmux control attach: %w", err)
	}

	cs := &ControlStream{
		cmd:    cmd,
		Events: make(chan StreamEvent, 256),
		done:   make(chan struct{}),
	}

	go func() {
		defer close(cs.Events)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "%output "):
				rest := line[len("%output "):]
				sp := strings.IndexByte(rest, ' ')
				if sp <= 0 {
					continue
				}
				select {
				case cs.Events <- StreamEvent{PaneID: rest[:sp], Data: decodeOctal(rest[sp+1:])}:
				case <-cs.done:
					return
				}
			case strings.HasPrefix(line, "%exit"):
				select {
				case cs.Events <- StreamEvent{Exit: true}:
				default:
				}
				return
			}
			// %begin/%end blocks, layout changes etc. are irrelevant to a viewer.
		}
	}()

	_ = stdin // held open: closing stdin detaches the control client
	return cs, nil
}

// Close detaches the control client.
func (cs *ControlStream) Close() {
	select {
	case <-cs.done:
		return
	default:
		close(cs.done)
	}
	if cs.cmd.Process != nil {
		cs.cmd.Process.Kill()
	}
	go cs.cmd.Wait()
}

// CaptureVisible returns the pane's current screen with ANSI preserved.
// Deliberately NOT scrollback: for full-screen TUIs, tmux history is a pile
// of intermediate redraw frames — replaying it renders garbage.
func CaptureVisible(target string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-e").Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

// CursorPos returns the pane's 0-based cursor position. Live deltas use
// absolute addressing, so the viewer must start with its cursor exactly
// where tmux has it or every subsequent overwrite lands on the wrong row.
func CursorPos(target string) (x, y int) {
	out, err := exec.Command("tmux", "display", "-p", "-t", target, "#{cursor_x} #{cursor_y}").Output()
	if err != nil {
		return 0, 0
	}
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &x, &y)
	return x, y
}

// decodeOctal reverses tmux control-mode escaping: control bytes arrive as
// \ooo (three octal digits) and backslash as \\.
func decodeOctal(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(s) && s[i+1] == '\\' {
			b.WriteByte('\\')
			i++
			continue
		}
		if i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			n, _ := strconv.ParseUint(s[i+1:i+4], 8, 8)
			b.WriteByte(byte(n))
			i += 3
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }
