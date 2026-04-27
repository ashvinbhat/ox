package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ashvinbhat/ox/internal/agent"
)

const (
	pollInterval   = 2 * time.Second
	sidebarWidth   = 32
	minPaneHeight  = 10
	captureLines   = 80
)

// Model is the bubbletea model for the agent TUI.
type Model struct {
	manager  *agent.Manager
	taskID   string
	registry *agent.AgentRegistry

	agents   []*agent.Agent
	selected int

	paneContent string
	showHelp    bool

	textInput textinput.Model
	inputMode bool
	btwMode   bool // when true, input is prefixed with /btw

	width  int
	height int

	err error
}

// tickMsg is sent on each poll interval.
type tickMsg time.Time

// paneMsg carries captured tmux pane content.
type paneMsg struct {
	content string
}

// registryMsg carries a refreshed registry.
type registryMsg struct {
	registry *agent.AgentRegistry
}

// NewModel creates a new TUI model.
func NewModel(mgr *agent.Manager, taskID string) Model {
	ti := textinput.New()
	ti.Placeholder = "type message to agent..."
	ti.CharLimit = 500

	return Model{
		manager:   mgr,
		taskID:    taskID,
		textInput: ti,
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshRegistry(),
		m.tickCmd(),
		tea.WindowSize(),
	)
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) refreshRegistry() tea.Cmd {
	return func() tea.Msg {
		m.manager.ReconcileStatus(m.taskID)
		reg, err := m.manager.LoadRegistry(m.taskID)
		if err != nil {
			return registryMsg{}
		}
		return registryMsg{registry: reg}
	}
}

func (m Model) capturePane() tea.Cmd {
	return func() tea.Msg {
		if len(m.agents) == 0 || m.selected >= len(m.agents) {
			return paneMsg{}
		}
		a := m.agents[m.selected]
		if a.TmuxSession == "" {
			return paneMsg{content: "(no tmux session)"}
		}
		content, err := captureAgentPane(a.TmuxSession, captureLines)
		if err != nil {
			return paneMsg{content: "(session not available)"}
		}
		return paneMsg{content: content}
	}
}
