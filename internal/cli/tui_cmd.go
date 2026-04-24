package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui [task-id]",
	Short: "Open the agent monitoring TUI",
	Long: `Opens an interactive terminal UI for monitoring and managing agents.

If a task ID is provided, shows agents for that task only.
Otherwise, shows agents for the most recent task with active agents.

Examples:
  ox tui        # monitor agents for most recent task
  ox tui 18     # monitor agents for task #18`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTui,
}

func runTui(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	var taskID string

	if len(args) > 0 {
		reg, err := findRegistryByRef(mgr, args[0])
		if err != nil {
			return err
		}
		taskID = reg.TaskID
	} else {
		// Find most recent task with agents
		registries, err := mgr.ListAllRegistries()
		if err != nil || len(registries) == 0 {
			return fmt.Errorf("no agents found — use 'ox spawn' or 'ox multi' first")
		}
		// Pick most recently created
		latest := registries[0]
		for _, reg := range registries[1:] {
			if reg.CreatedAt.After(latest.CreatedAt) {
				latest = reg
			}
		}
		taskID = latest.TaskID
	}

	return LaunchTUI(mgr, taskID)
}

// LaunchTUI starts the bubbletea TUI for monitoring agents.
func LaunchTUI(mgr *agent.Manager, taskID string) error {
	m := tui.NewModel(mgr, taskID)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
