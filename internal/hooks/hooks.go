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

	// Create built-in hooks
	builtins := m.BuiltInHooks()
	for _, h := range builtins {
		scriptPath := filepath.Join(m.hooksDir, h.Name+".sh")
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			content := m.generateBuiltInScript(h.Name)
			if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
				return fmt.Errorf("write hook %s: %w", h.Name, err)
			}
		}
		h.Script = scriptPath
		m.hooks[h.Name] = h
	}

	return nil
}

// BuiltInHooks returns the default built-in hooks.
func (m *Manager) BuiltInHooks() []*Hook {
	return []*Hook{
		{
			Name:        "yoke-ready-tasks",
			Description: "Show ready tasks from yoke at session start",
			Enabled:     true,
			BuiltIn:     true,
		},
		{
			Name:        "ox-instructions",
			Description: "ox CLI quick reference",
			Enabled:     true,
			BuiltIn:     true,
		},
		{
			Name:        "workspace-context",
			Description: "Current workspace and task summary",
			Enabled:     true,
			BuiltIn:     true,
		},
	}
}

// generateBuiltInScript creates the script content for a built-in hook.
func (m *Manager) generateBuiltInScript(name string) string {
	switch name {
	case "yoke-ready-tasks":
		return `#!/bin/bash
# ox hook: yoke-ready-tasks
# Shows ready tasks from yoke at Claude Code session start

INPUT=$(cat)

# Get ready tasks from yoke
READY_TASKS=$(yoke ready --short 2>/dev/null || echo "No ready tasks")

# Output JSON for Claude Code
jq -n --arg ctx "## Ready Tasks (from yoke)

$READY_TASKS

Use 'ox pickup <id> --repos <repo>' to start working on a task." '{
  hookSpecificOutput: {
    hookEventName: "SessionStart",
    additionalContext: $ctx
  }
}'
`
	case "ox-instructions":
		return `#!/bin/bash
# ox hook: ox-instructions
# Provides ox CLI quick reference

INPUT=$(cat)

INSTRUCTIONS="## ox CLI Reference

**Task Management:**
- ox pickup <id> --repos <repo>  # Create workspace for task
- ox status                       # Show current task
- ox done [id]                    # Complete task, cleanup

**During Work:**
- ox morph <persona>              # Switch persona (builder/explorer/reviewer/captain)
- ox skill inject <name>          # Add skill to workspace
- ox ship                         # Push and create PR

**Info:**
- ox tasks                        # List yoke tasks
- ox personas                     # List personas
- ox skill list                   # List skills"

jq -n --arg ctx "$INSTRUCTIONS" '{
  hookSpecificOutput: {
    hookEventName: "SessionStart",
    additionalContext: $ctx
  }
}'
`
	case "workspace-context":
		return `#!/bin/bash
# ox hook: workspace-context
# Shows current workspace context if in a workspace

INPUT=$(cat)

# Check if we're in an ox workspace
if [[ "$PWD" == *"/.ox/tasks/"* ]]; then
  WORKSPACE_NAME=$(basename "$PWD")
  TASK_ID=$(echo "$WORKSPACE_NAME" | cut -d'-' -f1)

  # Get task info from yoke
  TASK_INFO=$(yoke show "$TASK_ID" 2>/dev/null || echo "Task $TASK_ID")

  CONTEXT="## Current Workspace

Working on: $WORKSPACE_NAME
$TASK_INFO

Use 'ox morph <persona>' to change mindset, 'ox ship' when ready."

  jq -n --arg ctx "$CONTEXT" '{
    hookSpecificOutput: {
      hookEventName: "SessionStart",
      additionalContext: $ctx
    }
  }'
else
  # Not in workspace, output empty
  echo '{}'
fi
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

// ClaudeCodeHookEntry represents a hook entry in Claude Code settings.
type ClaudeCodeHookEntry struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"hooks"`
}

// GenerateClaudeCodeSettings generates the hooks section for Claude Code settings.
func (m *Manager) GenerateClaudeCodeSettings() ([]ClaudeCodeHookEntry, error) {
	hooks := m.List()
	if len(hooks) == 0 {
		return nil, nil
	}

	var entries []ClaudeCodeHookEntry

	// Create a single entry for all ox hooks
	entry := ClaudeCodeHookEntry{
		Matcher: "", // Empty matcher means all directories
	}

	for _, h := range hooks {
		if !h.Enabled {
			continue
		}
		entry.Hooks = append(entry.Hooks, struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}{
			Type:    "command",
			Command: h.Script,
		})
	}

	if len(entry.Hooks) > 0 {
		entries = append(entries, entry)
	}

	return entries, nil
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
	entries, err := m.GenerateClaudeCodeSettings()
	if err != nil {
		return err
	}

	// Update hooks section
	if len(entries) > 0 {
		settings["hooks"] = entries
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
