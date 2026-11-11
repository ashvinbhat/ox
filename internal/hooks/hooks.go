// Package hooks registers ox's Claude Code hooks in ~/.claude/settings.json.
//
// The one built-in, ox-brief, is `ox hook session-start`: ox itself, not a
// script, so it needs no jq and a panic or a slow brief prints nothing.
// Custom hooks are shell scripts in ~/.ox/hooks.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/livepath"
)

// Command is what Claude Code runs at every thread start. The desktop app's
// PATH may not reach ~/go/bin, where make install puts ox.
const Command = `/bin/sh -c 'exec "$(command -v ox || echo "$HOME/go/bin/ox")" hook session-start'`

// AskCommand runs before a thread asks Ashvin something (AskMatcher): it
// sends the question back unless it's a stop in ~/.ox/stops.md.
const AskCommand = `/bin/sh -c 'exec "$(command -v ox || echo "$HOME/go/bin/ox")" hook ask'`

// AskMatcher names the calls that ask Ashvin: the question dialog and the
// next-step box.
const AskMatcher = "AskUserQuestion|mcp__ccd_turn__wrap_up"

// askTimeout is short: the ask hook only reads a small file.
const askTimeout = 5

// Timeout is the seconds Claude Code gives the hook before killing it.
const Timeout = 15

// CheckEnv set to 1 runs the hook as ox setup's check: no background jobs,
// and the day's reminders stay for the first real thread.
const CheckEnv = "OX_HOOK_CHECK"

// WatchEnv at 1 in settings.json's env makes a desktop thread reload a plugin
// in ~/.claude/skills when its files change, so an ox setup reaches the board
// in threads already open. Desktop threads don't watch without it.
const WatchEnv = "CLAUDE_CODE_PLUGIN_DIR_WATCH"

// checkPath is the PATH a hook can count on: the desktop app may pass no more.
const checkPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// Hook is one SessionStart hook ox registers.
type Hook struct {
	Name        string
	Description string
	Enabled     bool
	Script      string // the command for the built-in, else the script's path
	BuiltIn     bool
}

// Manager registers ox's hooks.
type Manager struct {
	oxHome   string
	hooksDir string
	check    func() error
}

// NewManager creates a hook manager for the given OX_HOME.
func NewManager(oxHome string) *Manager {
	return &Manager{oxHome: oxHome, hooksDir: filepath.Join(oxHome, "hooks"), check: Check}
}

// retiredBuiltIns are scripts earlier ox versions installed. Every .sh in the
// hooks dir is registered, so a retired one left on disk would keep running.
// ox-brief.sh became `ox hook session-start`; the rest pointed threads at
// missions, which are frozen.
var retiredBuiltIns = []string{"ox-brief", "ox-instructions", "yoke-ready-tasks", "workspace-context"}

// Init creates the hooks dir and removes retired scripts. Setup runs it only
// once the new hook passed its check, so a failed check leaves the old script
// for the restored settings to run.
func (m *Manager) Init() error {
	if err := os.MkdirAll(m.hooksDir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	for _, name := range retiredBuiltIns {
		if err := os.Remove(filepath.Join(m.hooksDir, name+".sh")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove retired hook %s: %w", name, err)
		}
	}
	return nil
}

// BuiltInHooks returns the hooks ox ships.
func (m *Manager) BuiltInHooks() []*Hook {
	return []*Hook{{
		Name:        "ox-brief",
		Description: "Start every thread with `ox now --brief`",
		Enabled:     true,
		Script:      Command,
		BuiltIn:     true,
	}}
}

// List returns the built-in hook, then each custom script in the hooks dir.
func (m *Manager) List() []*Hook {
	hooks := m.BuiltInHooks()
	entries, err := os.ReadDir(m.hooksDir)
	if err != nil {
		return hooks
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".sh")
		if e.IsDir() || !ok || slices.Contains(retiredBuiltIns, name) {
			continue
		}
		hooks = append(hooks, &Hook{Name: name, Script: filepath.Join(m.hooksDir, e.Name()), Enabled: true})
	}
	return hooks
}

// Get returns a hook by name.
func (m *Manager) Get(name string) (*Hook, bool) {
	for _, h := range m.List() {
		if h.Name == name {
			return h, true
		}
	}
	return nil, false
}

// Run executes a hook with input on stdin and returns what it printed.
func (m *Manager) Run(name string, input string) (string, error) {
	h, ok := m.Get(name)
	if !ok {
		return "", fmt.Errorf("hook %q not found", name)
	}
	cmd := exec.Command("bash", h.Script)
	if h.BuiltIn {
		cmd = exec.Command("/bin/sh", "-c", h.Script)
	}
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run hook: %w", err)
	}
	return string(out), nil
}

// HooksDir returns the hooks directory.
func (m *Manager) HooksDir() string {
	return m.hooksDir
}

// SettingsPath is Claude Code's user settings file.
func SettingsPath() (string, error) {
	home, err := livepath.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Setup registers ox's hooks and runs the start hook the way a thread would.
// When the check fails, settings.json goes back to the bytes it had.
func (m *Manager) Setup(settingsPath string) error {
	prev, readErr := os.ReadFile(settingsPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := m.Install(settingsPath); err != nil {
		return err
	}
	if err := m.check(); err != nil {
		if readErr == nil {
			_ = writeAtomic(settingsPath, prev)
		} else {
			_ = os.Remove(settingsPath)
		}
		return fmt.Errorf("the start hook failed its check, so %s is back as it was: %w", settingsPath, err)
	}
	return m.Init()
}

// Install writes ox's SessionStart entry into settings.json, replacing any
// earlier ox entry and keeping every other hook and setting.
func (m *Manager) Install(settingsPath string) error {
	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("%s isn't valid JSON, so ox left it alone: %w", settingsPath, err)
		}
	}
	events, _ := settings["hooks"].(map[string]any)
	if events == nil {
		events = map[string]any{}
	}
	kept := m.withoutOurs(events["SessionStart"])
	var ours []any
	for _, h := range m.List() {
		handler := map[string]any{"type": "command", "command": h.Script}
		if h.BuiltIn {
			handler["timeout"] = Timeout
		}
		ours = append(ours, handler)
	}
	events["SessionStart"] = append(kept, map[string]any{"matcher": "", "hooks": ours})
	events["PreToolUse"] = append(m.withoutOurs(events["PreToolUse"]), map[string]any{"matcher": AskMatcher,
		"hooks": []any{map[string]any{"type": "command", "command": AskCommand, "timeout": askTimeout}}})
	settings["hooks"] = events
	// A value Ashvin set, 0 included, stays: Watching reports it.
	switch env := settings["env"].(type) {
	case nil:
		settings["env"] = map[string]any{WatchEnv: "1"}
	case map[string]any:
		if _, set := env[WatchEnv]; !set {
			env[WatchEnv] = "1"
		}
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return writeAtomic(settingsPath, append(data, '\n'))
}

// Watching is WatchEnv's value in settings.json's env, "" when it isn't set.
func Watching(settingsPath string) string {
	var settings struct {
		Env map[string]any `json:"env"`
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil || json.Unmarshal(data, &settings) != nil {
		return ""
	}
	if v, ok := settings.Env[WatchEnv]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// withoutOurs is an event's hook groups with ox's handlers taken out, every
// other handler kept in its group.
func (m *Manager) withoutOurs(event any) []any {
	groups, _ := event.([]any)
	kept := []any{}
	for _, g := range groups {
		group, ok := g.(map[string]any)
		handlers, _ := group["hooks"].([]any)
		if !ok || len(handlers) == 0 {
			kept = append(kept, g)
			continue
		}
		var rest []any
		for _, h := range handlers {
			if handler, ok := h.(map[string]any); ok {
				if c, _ := handler["command"].(string); m.owns(c) {
					continue
				}
			}
			rest = append(rest, h)
		}
		if len(rest) > 0 {
			group["hooks"] = rest
			kept = append(kept, group)
		}
	}
	return kept
}

// InstallToClaudeCode installs into ~/.claude/settings.json.
func (m *Manager) InstallToClaudeCode() error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	return m.Install(path)
}

// owns reports a hook command ox registered: the built-in, a script in the
// hooks dir, or a pre-1.0 script under another OX_HOME.
func (m *Manager) owns(command string) bool {
	return command == Command || command == AskCommand ||
		strings.HasPrefix(command, m.hooksDir+string(filepath.Separator)) ||
		strings.Contains(command, "/.ox/hooks/")
}

// Check runs the start hook as Claude Code would, with the bare PATH and a
// SessionStart payload, and wants the brief back as SessionStart context.
func Check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*Timeout*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", Command)
	home, _ := livepath.Home()
	cmd.Env = []string{"PATH=" + checkPath, "HOME=" + home, CheckEnv + "=1"}
	if v := os.Getenv("OX_HOME"); v != "" {
		cmd.Env = append(cmd.Env, "OX_HOME="+v)
	}
	cmd.Stdin = strings.NewReader(`{"session_id":"ox-setup-check","hook_event_name":"SessionStart","source":"startup"}`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("it ran past %ds", 2*Timeout)
	case err != nil:
		return fmt.Errorf("%v: %s", err, firstLine(stderr.String()))
	case len(bytes.TrimSpace(out)) == 0:
		return errors.New("it printed nothing: no ox config at the default OX_HOME, or the brief took over 10s")
	}
	var got struct {
		Out struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return fmt.Errorf("it printed something that isn't JSON: %s", firstLine(string(out)))
	}
	if got.Out.Event != "SessionStart" || !strings.Contains(got.Out.Context, "ox now") {
		return fmt.Errorf("it printed no brief: %s", firstLine(string(out)))
	}
	return checkAsk(home)
}

// checkAsk runs the ask hook on a question that isn't a stop and wants it
// sent back with the list.
func checkAsk(home string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*askTimeout*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", AskCommand)
	cmd.Env = []string{"PATH=" + checkPath, "HOME=" + home, CheckEnv + "=1"}
	if v := os.Getenv("OX_HOME"); v != "" {
		cmd.Env = append(cmd.Env, "OX_HOME="+v)
	}
	cmd.Stdin = strings.NewReader(`{"session_id":"ox-setup-check","tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"Commit this?"}}`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), "Sent back") {
		return fmt.Errorf("the ask hook didn't send a plain question back (%v): %s", err, firstLine(stderr.String()))
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".ox-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	return line
}
