package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

// View renders the TUI.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	if m.showHelp {
		return m.helpView()
	}

	// Layout: sidebar | pane
	//         input bar
	//         status bar

	statusBar := m.statusBarView()
	inputBar := m.inputBarView()

	// Calculate available height for main content
	mainHeight := m.height - lipgloss.Height(statusBar) - lipgloss.Height(inputBar) - 2

	sidebar := m.sidebarView(mainHeight)
	pane := m.paneView(mainHeight)

	// Join sidebar and pane horizontally
	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, pane)

	return lipgloss.JoinVertical(lipgloss.Left, main, inputBar, statusBar)
}

func (m Model) sidebarView(height int) string {
	w := sidebarWidth

	var lines []string

	// Title
	title := "Agents"
	if m.registry != nil {
		title = fmt.Sprintf("Task #%d", m.registry.TaskSeq)
	}
	lines = append(lines, sidebarTitleStyle.Render(title))
	lines = append(lines, "")

	if len(m.agents) == 0 {
		lines = append(lines, pendingStyle.Render("  No agents"))
	}

	for i, a := range m.agents {
		icon := agentStatusIcon(a)
		status := string(a.Status)

		// Duration
		dur := time.Since(a.SpawnedAt).Truncate(time.Second)
		if a.FinishedAt != nil {
			dur = a.FinishedAt.Sub(a.SpawnedAt).Truncate(time.Second)
		}

		name := a.ID
		if len(name) > 18 {
			name = name[:17] + "…"
		}

		line := fmt.Sprintf(" %s %-18s %s", icon, name, formatDuration(dur))

		if i == m.selected {
			line = agentSelectedStyle.Width(w - 2).Render(line)
		} else {
			line = agentNormalStyle.Render(line)
		}

		lines = append(lines, line)

		// Show details under selected
		if i == m.selected {
			detail := fmt.Sprintf("   %s | %s | %s", a.Persona, modelLabel(a.Model), status)
			lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render(detail))
		}
	}

	content := strings.Join(lines, "\n")

	return sidebarStyle.
		Width(w).
		Height(height).
		Render(content)
}

func (m Model) paneView(height int) string {
	paneWidth := m.width - sidebarWidth - 4 // account for borders
	if paneWidth < 20 {
		paneWidth = 20
	}

	var title string
	if len(m.agents) > 0 && m.selected < len(m.agents) {
		a := m.agents[m.selected]
		live := ""
		if tmuxutil.HasSession(a.TmuxSession) {
			live = " (live)"
		}
		title = fmt.Sprintf("%s (%s)%s", a.ID, a.Persona, live)
	} else {
		title = "No agent selected"
	}

	titleLine := paneTitleStyle.Render(title)

	// Trim pane content to fit
	content := m.paneContent
	contentLines := strings.Split(content, "\n")

	// Limit to available height
	maxLines := height - 2
	if maxLines < 1 {
		maxLines = 1
	}
	if len(contentLines) > maxLines {
		contentLines = contentLines[len(contentLines)-maxLines:]
	}
	content = strings.Join(contentLines, "\n")

	fullContent := titleLine + "\n" + content

	return paneStyle.
		Width(paneWidth).
		Height(height).
		Render(fullContent)
}

func (m Model) inputBarView() string {
	w := m.width - 2

	if m.inputMode {
		m.textInput.Width = w - 4
		return inputStyle.Width(w).Render(m.textInput.View())
	}

	prompt := statusBarStyle.Render("  Press [Enter] to message · [Tab] switch · [a] attach · [x] kill · [?] help · [q] quit")
	return lipgloss.NewStyle().Width(w).Render(prompt)
}

func (m Model) statusBarView() string {
	running := 0
	done := 0
	total := len(m.agents)
	for _, a := range m.agents {
		switch a.Status {
		case agent.StatusRunning:
			running++
		case agent.StatusDone:
			done++
		}
	}

	left := fmt.Sprintf(" %d agents · %d running · %d done", total, running, done)
	right := "polling every 2s"

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return statusBarStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) helpView() string {
	help := `
  ox agent monitor — Keybindings

  Tab / j / ↓     Select next agent
  Shift+Tab / k / ↑  Select previous agent
  Enter / i       Start typing a message
  a               Attach to agent (tmux)
  x               Kill selected agent
  r               Refresh
  ?               Toggle this help
  q / Ctrl+C      Quit (agents keep running)

  In input mode:
  Enter           Send message
  Esc             Cancel input

  Press any key to close help...
`
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Width(m.width - 4).
		Height(m.height - 2).
		Render(help)
}

func agentStatusIcon(a *agent.Agent) string {
	switch a.Status {
	case agent.StatusRunning:
		if tmuxutil.HasSession(a.TmuxSession) {
			return runningStyle.Render("●")
		}
		return doneStyle.Render("✓")
	case agent.StatusDone:
		return doneStyle.Render("✓")
	case agent.StatusFailed:
		return failedStyle.Render("✗")
	case agent.StatusKilled:
		return killedStyle.Render("⊘")
	case agent.StatusPending:
		return pendingStyle.Render("○")
	case agent.StatusIdle:
		return idleStyle.Render("◐")
	default:
		return pendingStyle.Render("?")
	}
}

func modelLabel(model string) string {
	if model == "" {
		return "default"
	}
	return model
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
