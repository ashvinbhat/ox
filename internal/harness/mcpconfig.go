package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/mission"
)

type mcpConfig struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// oxBinary resolves the running ox executable so generated configs work in
// tmux windows whose PATH may lack ~/go/bin.
func oxBinary() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "ox"
}

// WriteMCPConfig generates the mission's MCP wiring: the orchestrator config
// used via --mcp-config, and .claude/settings.local.json so a bare `claude`
// typed by hand in the mission dir picks the server up without prompts.
func WriteMCPConfig(m *mission.Mission) error {
	cfg := mcpConfig{MCPServers: map[string]mcpServerEntry{
		"ox": {
			Command: oxBinary(),
			Args:    []string{"mcp", "--mission", m.ID, "--role", "orchestrator"},
		},
	}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.Dir(), ".mcp.json"), data, 0o644); err != nil {
		return fmt.Errorf("write .mcp.json: %w", err)
	}

	settingsDir := filepath.Join(m.Dir(), ".claude")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return err
	}
	// The UserPromptSubmit hook is the event-delivery channel: unread mission
	// events ride invisibly on the user's own messages instead of being typed
	// into the conversation.
	settings := map[string]any{
		"enableAllProjectMcpServers": true,
		"hooks": map[string]any{
			"UserPromptSubmit": []map[string]any{{
				"hooks": []map[string]any{{
					"type":    "command",
					"command": fmt.Sprintf("'%s' events attach --mission %s", oxBinary(), m.ID),
				}},
			}},
		},
	}
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(settingsDir, "settings.local.json"), data, 0o644)
}

// WriteWorkerMCPConfig generates a worker-scoped config at
// workers/<id>/mcp.json for use with --mcp-config --strict-mcp-config.
func WriteWorkerMCPConfig(m *mission.Mission, agentID string) (string, error) {
	dir := filepath.Join(m.Dir(), "workers", agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	cfg := mcpConfig{MCPServers: map[string]mcpServerEntry{
		"ox": {
			Command: oxBinary(),
			Args:    []string{"mcp", "--mission", m.ID, "--role", "worker", "--agent", agentID},
		},
	}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write worker mcp.json: %w", err)
	}
	return path, nil
}
