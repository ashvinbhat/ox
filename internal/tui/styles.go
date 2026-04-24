package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	colorGreen  = lipgloss.Color("#22c55e")
	colorRed    = lipgloss.Color("#ef4444")
	colorYellow = lipgloss.Color("#eab308")
	colorBlue   = lipgloss.Color("#3b82f6")
	colorPurple = lipgloss.Color("#a855f7")
	colorGray   = lipgloss.Color("#6b7280")
	colorDim    = lipgloss.Color("#4b5563")
	colorWhite  = lipgloss.Color("#f9fafb")

	// Sidebar styles
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGray).
			Padding(0, 1)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorWhite)

	agentSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e3a5f")).
				Foreground(colorWhite).
				Bold(true)

	agentNormalStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	// Pane styles
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGray).
			Padding(0, 1)

	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue)

	// Status bar styles
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	// Input styles
	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGray).
			Padding(0, 1)

	// Status icon styles
	runningStyle = lipgloss.NewStyle().Foreground(colorGreen)
	doneStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	failedStyle  = lipgloss.NewStyle().Foreground(colorRed)
	killedStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	pendingStyle = lipgloss.NewStyle().Foreground(colorGray)
	idleStyle    = lipgloss.NewStyle().Foreground(colorPurple)
)
