// Package hooks manages Claude Code hooks for context injection.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Hook represents a hook script that injects context into Claude Code.
type Hook struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Enabled     bool   `yaml:"enabled"`
	Script      string `yaml:"-"` // Path to script
	BuiltIn     bool   `yaml:"-"` // Is this a built-in hook?
}

// Manager manages hooks for ox.
type Manager struct {
	oxHome   string
	hooksDir string
	hooks    map[string]*Hook
}

// NewManager creates a new hook manager.
func NewManager(oxHome string) *Manager {
	return &Manager{
		oxHome:   oxHome,
		hooksDir: filepath.Join(oxHome, "hooks"),
		hooks:    make(map[string]*Hook),
	}
}

// Init initializes the hooks directory and built-in hooks.
func (m *Manager) Init() error {
	if err := os.MkdirAll(m.hooksDir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}

	// Every .sh in the hooks dir gets registered, so a retired built-in left on
	// disk would keep running at every session start.
	for _, name := range retiredBuiltIns {
		if err := os.Remove(filepath.Join(m.hooksDir, name+".sh")); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove retired hook %s: %w", name, err)
		}
	}

	// Built-in scripts are owned by ox: rewrite them whenever the embedded
	// content changes, otherwise stale copies advertise removed commands.
	builtins := m.BuiltInHooks()
	for _, h := range builtins {
		scriptPath := filepath.Join(m.hooksDir, h.Name+".sh")
		content := m.generateBuiltInScript(h.Name)
		existing, err := os.ReadFile(scriptPath)
		if err != nil || string(existing) != content {
			if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
				return fmt.Errorf("write hook %s: %w", h.Name, err)
			}
		}
		h.Script = scriptPath
		m.hooks[h.Name] = h
	}

	return nil
}

// retiredBuiltIns are hooks earlier ox versions installed. They pointed
// threads at missions, which are frozen in 1.0.
var retiredBuiltIns = []string{"ox-instructions", "yoke-ready-tasks", "workspace-context"}

// BuiltInHooks returns the default built-in hooks.
func (m *Manager) BuiltInHooks() []*Hook {
	return []*Hook{
		{
			Name:        "ox-brief",
			Description: "Start every thread with `ox now --brief`",
			Enabled:     true,
			BuiltIn:     true,
		},
	}
}

// generateBuiltInScript creates the script content for a built-in hook.
func (m *Manager) generateBuiltInScript(name string) string {
	switch name {
	case "ox-brief":
		// Hooks run without the user's shell PATH, hence the go/bin fallback.
		// Any failure prints nothing, so a broken ox never blocks a session.
		// The worktree prune runs detached so its gh calls never delay the brief.
		return `#!/bin/bash
# ox hook: ox-brief
# Starts every Claude Code thread with where things stand (ox now --brief),
# and removes worktrees whose PR merged (at most hourly, in the background).

cat > /dev/null

OX=$(command -v ox || echo "$HOME/go/bin/ox")
("$OX" worktrees prune --if-due >/dev/null 2>&1 &)
BRIEF=$("$OX" now --brief 2>/dev/null) || exit 0
[ -z "$BRIEF" ] && exit 0

jq -n --arg ctx "$BRIEF" '{
  hookSpecificOutput: {
    hookEventName: "SessionStart",
    additionalContext: $ctx
  }
}'
`
	default:
		return `#!/bin/bash
# Custom hook: ` + name + `
INPUT=$(cat)
echo '{}'
`
	}
}

// List returns all available hooks.
func (m *Manager) List() []*Hook {
	// Load from directory
	entries, err := os.ReadDir(m.hooksDir)
	if err != nil {
		return m.BuiltInHooks()
	}

	var hooks []*Hook
	builtinNames := make(map[string]bool)
	for _, h := range m.BuiltInHooks() {
		builtinNames[h.Name] = true
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".sh")
		hooks = append(hooks, &Hook{
			Name:    name,
			Script:  filepath.Join(m.hooksDir, e.Name()),
			BuiltIn: builtinNames[name],
			Enabled: true, // TODO: Check config
		})
	}

	return hooks
}

// Get returns a hook by name.
func (m *Manager) Get(name string) (*Hook, bool) {
	scriptPath := filepath.Join(m.hooksDir, name+".sh")
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, false
	}

	builtinNames := make(map[string]bool)
	for _, h := range m.BuiltInHooks() {
		builtinNames[h.Name] = true
	}

	return &Hook{
		Name:    name,
		Script:  scriptPath,
		BuiltIn: builtinNames[name],
		Enabled: true,
	}, true
}

// Run executes a hook and returns its output.
func (m *Manager) Run(name string, input string) (string, error) {
	hook, ok := m.Get(name)
	if !ok {
		return "", fmt.Errorf("hook %q not found", name)
	}

	cmd := exec.Command("bash", hook.Script)
	cmd.Stdin = strings.NewReader(input)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run hook: %w", err)
	}

	return string(output), nil
}

// HooksDir returns the hooks directory.
func (m *Manager) HooksDir() string {
	return m.hooksDir
}

// ClaudeCodeHookHandler represents a single hook handler.
type ClaudeCodeHookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// ClaudeCodeMatcherGroup represents a matcher group with its hooks.
type ClaudeCodeMatcherGroup struct {
	Matcher string                  `json:"matcher"`
	Hooks   []ClaudeCodeHookHandler `json:"hooks"`
}

// ClaudeCodeHooks represents the new hooks format keyed by event name.
type ClaudeCodeHooks map[string][]ClaudeCodeMatcherGroup

// GenerateClaudeCodeSettings generates the hooks section for Claude Code settings.
func (m *Manager) GenerateClaudeCodeSettings() (ClaudeCodeHooks, error) {
	hooks := m.List()
	if len(hooks) == 0 {
		return nil, nil
	}

	// Create handlers for all enabled hooks
	var handlers []ClaudeCodeHookHandler
	for _, h := range hooks {
		if !h.Enabled {
			continue
		}
		handlers = append(handlers, ClaudeCodeHookHandler{
			Type:    "command",
			Command: h.Script,
		})
	}

	if len(handlers) == 0 {
		return nil, nil
	}

	// Create matcher group (empty matcher = match all)
	matcherGroup := ClaudeCodeMatcherGroup{
		Matcher: "",
		Hooks:   handlers,
	}

	// Return hooks keyed by SessionStart event
	return ClaudeCodeHooks{
		"SessionStart": []ClaudeCodeMatcherGroup{matcherGroup},
	}, nil
}

// InstallToClaudeCode adds ox hooks to Claude Code settings.json.
func (m *Manager) InstallToClaudeCode() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Read existing settings
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Generate hook entries
	hooksConfig, err := m.GenerateClaudeCodeSettings()
	if err != nil {
		return err
	}

	// Update hooks section (new format: object keyed by event name)
	if len(hooksConfig) > 0 {
		// Get existing hooks or create new map
		existingHooks, ok := settings["hooks"].(map[string]interface{})
		if !ok {
			existingHooks = make(map[string]interface{})
		}

		// Merge ox hooks into SessionStart
		existingHooks["SessionStart"] = hooksConfig["SessionStart"]
		settings["hooks"] = existingHooks
	}

	// Ensure .claude directory exists
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}

	// Write back
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(settingsPath, data, 0o644)
}
