// Package harness is the engine of the mission runtime: prompt assembly for
// the orchestrator and workers, and (in later layers) worker spawning,
// merging, and shipping. The CLI, MCP server, and watcher all call into it.
package harness

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/mission"
)

//go:embed prompts/*.md
var promptFS embed.FS

func embedded(name string) string {
	data, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		return ""
	}
	return string(data)
}

// PlaybookBody returns the playbook markdown for a mission type. A user file
// at ~/.ox/playbooks/<type>.md overrides the embedded default, so new mission
// types (and tweaks to existing ones) need no Go changes.
func PlaybookBody(oxHome, typ string) (string, error) {
	userPath := filepath.Join(oxHome, "playbooks", typ+".md")
	if data, err := os.ReadFile(userPath); err == nil {
		return string(data), nil
	}
	if body := embedded("playbook-" + typ + ".md"); body != "" {
		return body, nil
	}
	return "", fmt.Errorf("no playbook %q (looked in %s and embedded defaults)", typ, userPath)
}

// PlaybookExists reports whether a mission type resolves to a playbook.
func PlaybookExists(oxHome, typ string) bool {
	_, err := PlaybookBody(oxHome, typ)
	return err == nil
}

// BuildOrchestratorPrompt assembles the orchestrator's --append-system-prompt:
// harness core + hygiene + playbook + a generated mission header. It must stay
// byte-stable for a given mission so prompt caching holds across resumes.
func BuildOrchestratorPrompt(oxHome string, m *mission.Mission) (string, error) {
	playbook, err := PlaybookBody(oxHome, m.Type)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(embedded("harness-core.md"))
	sb.WriteString("\n")
	sb.WriteString(embedded("hygiene.md"))
	sb.WriteString("\n---\n\n")
	sb.WriteString(playbook)
	sb.WriteString("\n---\n\n")
	sb.WriteString(missionHeader(m))
	return sb.String(), nil
}

// missionHeader is the small mission-specific block. It is also mirrored into
// AGENTS.md so a compacted/restarted orchestrator can re-read it from disk.
func missionHeader(m *mission.Mission) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Mission %s: %s\n\n", m.ID, m.Goal)
	fmt.Fprintf(&sb, "- Mission dir: %s (plan.md, decisions.md, scratchpad.md, workers/, jobs/)\n", m.Dir())
	if m.Yoke != nil {
		fmt.Fprintf(&sb, "- Task: #%d in the local tracker — `yoke context %d` for full history\n", m.Yoke.Seq, m.Yoke.Seq)
	} else {
		sb.WriteString("- No linked task — freeform mission\n")
	}
	fmt.Fprintf(&sb, "- Playbook: %s · phase at launch: %s\n", m.Type, m.Phase)
	fmt.Fprintf(&sb, "- Budget: $%.2f mission / $%.2f per agent / $%.2f per job\n",
		m.Budgets.MissionUSD, m.Budgets.PerAgentUSD, m.Budgets.PerJobUSD)
	fmt.Fprintf(&sb, "- Max parallel agents: %d\n", m.Approvals.MaxParallelAgents)
	sb.WriteString("- Base repo clones (read-only exploration): ~/.ox/repos/<name>\n")
	return sb.String()
}

// WriteOrchestratorFiles materializes the mission-dir documents the
// orchestrator and any manual `claude` session rely on: the prompt file
// consumed at launch, AGENTS.md (mission header + task context), and the
// CLAUDE.md symlink.
func WriteOrchestratorFiles(oxHome string, m *mission.Mission, taskContextMD string) error {
	prompt, err := BuildOrchestratorPrompt(oxHome, m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.Dir(), "orchestrator-prompt.md"), []byte(prompt), 0o644); err != nil {
		return fmt.Errorf("write orchestrator prompt: %w", err)
	}

	var agents strings.Builder
	agents.WriteString(missionHeader(m))
	if taskContextMD != "" {
		agents.WriteString("\n---\n\n")
		agents.WriteString(taskContextMD)
	}
	if err := os.WriteFile(filepath.Join(m.Dir(), "AGENTS.md"), []byte(agents.String()), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	claudePath := filepath.Join(m.Dir(), "CLAUDE.md")
	os.Remove(claudePath)
	if err := os.Symlink("AGENTS.md", claudePath); err != nil {
		return fmt.Errorf("link CLAUDE.md: %w", err)
	}
	return nil
}
