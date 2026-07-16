// Package cmux mirrors missions into the cmux terminal as a native viewport:
// one workspace per mission (orc attached), a split per live worker, phase
// chips, and notifications. Everything is best-effort over the cmux CLI —
// when the app is closed or the binary absent, ox behaves as if this package
// didn't exist. tmux remains the substrate; cmux panes only attach to it.
package cmux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

const maxAutoSplits = 3

// state maps a mission to its cmux workspace and worker splits by UUID —
// refs (workspace:2) shift as workspaces close, UUIDs don't.
type state struct {
	Workspace string            `json:"workspace"`
	Surfaces  map[string]string `json:"surfaces,omitempty"` // worker id → surface uuid
}

func statePath(m *mission.Mission) string { return filepath.Join(m.Dir(), ".cmux.json") }

func loadState(m *mission.Mission) *state {
	st := &state{Surfaces: map[string]string{}}
	if data, err := os.ReadFile(statePath(m)); err == nil {
		json.Unmarshal(data, st)
		if st.Surfaces == nil {
			st.Surfaces = map[string]string{}
		}
	}
	return st
}

func saveState(m *mission.Mission, st *state) {
	if data, err := json.MarshalIndent(st, "", "  "); err == nil {
		os.WriteFile(statePath(m), data, 0o644)
	}
}

func run(args ...string) (string, error) {
	bin, err := exec.LookPath("cmux")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "CMUX_QUIET=1")
	if os.Getenv("CMUX_SOCKET_PASSWORD") == "" {
		if pw := secretsValue("CMUX_SOCKET_PASSWORD"); pw != "" {
			cmd.Env = append(cmd.Env, "CMUX_SOCKET_PASSWORD="+pw)
		}
	}
	done := make(chan struct{})
	var out []byte
	go func() { out, err = cmd.CombinedOutput(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		return "", fmt.Errorf("cmux timed out")
	}
	if err != nil {
		return "", fmt.Errorf("cmux %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func secretsValue(key string) string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".ox", "secrets.env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// Available reports whether the cmux app is reachable. Cheap enough for a
// 30s tick; LookPath alone catches most-absent fast.
func Available() bool {
	if _, err := exec.LookPath("cmux"); err != nil {
		return false
	}
	_, err := run("ping")
	return err == nil
}

var uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func workspaceName(m *mission.Mission) string {
	goal := m.Goal
	if len(goal) > 34 {
		goal = goal[:34]
	}
	return fmt.Sprintf("%s · %s", m.ID, goal)
}

// findWorkspace resolves the mission's workspace UUID: by recorded uuid if
// still alive, else by name prefix (adopts hand-created workspaces).
func findWorkspace(m *mission.Mission, st *state) string {
	out, err := run("list-workspaces", "--id-format", "both")
	if err != nil {
		return ""
	}
	prefix := m.ID + " ·"
	var byName string
	for _, line := range strings.Split(out, "\n") {
		uuid := uuidRe.FindString(line)
		if uuid == "" {
			continue
		}
		if st.Workspace != "" && strings.Contains(line, st.Workspace) {
			return st.Workspace
		}
		if strings.Contains(line, prefix) && byName == "" {
			byName = uuid
		}
	}
	return byName
}

// SyncMission converges the cmux workspace with mission reality: ensure the
// workspace, a split per live worker (capped), close splits for reaped
// workers, refresh the phase chip. Safe to call every tick.
func SyncMission(cfg *config.Config, m *mission.Mission) {
	if !Available() {
		return
	}
	st := loadState(m)

	ws := findWorkspace(m, st)
	if ws == "" {
		if _, err := run("new-workspace",
			"--name", workspaceName(m),
			"--cwd", m.Dir(),
			"--command", fmt.Sprintf("tmux attach -t %s", m.TmuxSession()),
			"--focus", "false"); err != nil {
			return
		}
		ws = findWorkspace(m, st)
		if ws == "" {
			return
		}
	}
	if ws != st.Workspace {
		st.Workspace = ws
		saveState(m, st)
	}

	run("set-status", "phase", m.Phase, "--workspace", ws, "--icon", "gearshape", "--color", "#7aa2f7")

	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return
	}
	live := map[string]string{} // worker id → tmux session
	for id, w := range reg.Workers {
		if tmuxutil.HasSession(w.TmuxSession) {
			live[id] = w.TmuxSession
		}
	}

	changed := false
	for id, surface := range st.Surfaces {
		if _, ok := live[id]; !ok {
			run("close-surface", "--surface", surface, "--workspace", ws)
			delete(st.Surfaces, id)
			changed = true
		}
	}
	for id, sess := range live {
		if _, ok := st.Surfaces[id]; ok || len(st.Surfaces) >= maxAutoSplits {
			continue
		}
		if surface := addSplit(ws, sess); surface != "" {
			st.Surfaces[id] = surface
			changed = true
		}
	}
	if changed {
		saveState(m, st)
	}
}

// addSplit opens a split in the workspace and attaches the given tmux
// session in it, returning the new surface's uuid.
func addSplit(ws, tmuxSession string) string {
	out, err := run("new-split", "right", "--workspace", ws, "--focus", "false", "--id-format", "both")
	if err != nil {
		return ""
	}
	surface := uuidRe.FindString(out)
	if surface == "" {
		return ""
	}
	// Fresh split spawns a shell; give it a beat before typing into it.
	time.Sleep(700 * time.Millisecond)
	if _, err := run("send", "--surface", surface, "--workspace", ws, fmt.Sprintf("tmux attach -t %s", tmuxSession)); err != nil {
		return surface
	}
	run("send-key", "--surface", surface, "--workspace", ws, "enter")
	return surface
}

// Notify raises a native notification on the mission's workspace and flashes
// its pane — the cmux face of a watcher wake-up.
func Notify(m *mission.Mission, title, body string) {
	if !Available() {
		return
	}
	st := loadState(m)
	if st.Workspace == "" {
		return
	}
	run("notify", "--title", title, "--body", body, "--workspace", st.Workspace)
	run("trigger-flash", "--workspace", st.Workspace)
}

// CloseMission removes the mission's workspace at close.
func CloseMission(m *mission.Mission) {
	if !Available() {
		return
	}
	st := loadState(m)
	if st.Workspace == "" {
		return
	}
	run("close-workspace", "--workspace", st.Workspace)
	os.Remove(statePath(m))
}
