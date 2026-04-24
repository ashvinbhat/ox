package tui

import (
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		cmds = append(cmds, m.refreshRegistry())
		cmds = append(cmds, m.capturePane())
		cmds = append(cmds, m.tickCmd())
		return m, tea.Batch(cmds...)

	case registryMsg:
		if msg.registry != nil {
			m.registry = msg.registry
			m.agents = msg.registry.Agents
			if m.selected >= len(m.agents) && len(m.agents) > 0 {
				m.selected = len(m.agents) - 1
			}
		}
		return m, nil

	case paneMsg:
		m.paneContent = msg.content
		return m, nil
	}

	if m.inputMode {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Input mode handling
	if m.inputMode {
		switch msg.String() {
		case "enter":
			text := m.textInput.Value()
			if text != "" && len(m.agents) > 0 && m.selected < len(m.agents) {
				a := m.agents[m.selected]
				tmuxutil.SendKeys(a.TmuxSession, text)
			}
			m.textInput.Reset()
			m.inputMode = false
			m.textInput.Blur()
			return m, nil
		case "esc":
			m.inputMode = false
			m.textInput.Reset()
			m.textInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}
	}

	// Normal mode handling
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "tab", "j", "down":
		if len(m.agents) > 0 {
			m.selected = (m.selected + 1) % len(m.agents)
			m.paneContent = ""
			return m, m.capturePane()
		}

	case "shift+tab", "k", "up":
		if len(m.agents) > 0 {
			m.selected = (m.selected - 1 + len(m.agents)) % len(m.agents)
			m.paneContent = ""
			return m, m.capturePane()
		}

	case "enter", "i":
		m.inputMode = true
		m.textInput.Focus()
		return m, textinput.Blink

	case "a":
		// Attach to selected agent
		if len(m.agents) > 0 && m.selected < len(m.agents) {
			a := m.agents[m.selected]
			if tmuxutil.HasSession(a.TmuxSession) {
				return m, tea.ExecProcess(
					exec.Command("tmux", "attach", "-t", a.TmuxSession),
					func(err error) tea.Msg { return tickMsg(time.Now()) },
				)
			}
		}

	case "x":
		// Kill selected agent
		if len(m.agents) > 0 && m.selected < len(m.agents) {
			a := m.agents[m.selected]
			if tmuxutil.HasSession(a.TmuxSession) {
				tmuxutil.KillSession(a.TmuxSession)
			}
			now := time.Now()
			m.manager.UpdateAgent(m.taskID, a.ID, func(a *agent.Agent) {
				a.Status = agent.StatusKilled
				a.FinishedAt = &now
			})
			return m, m.refreshRegistry()
		}

	case "?":
		m.showHelp = !m.showHelp

	case "r":
		return m, tea.Batch(m.refreshRegistry(), m.capturePane())
	}

	return m, nil
}

// captureAgentPane captures tmux pane content for an agent.
func captureAgentPane(session string, lines int) (string, error) {
	return tmuxutil.CapturePane(session, lines)
}
