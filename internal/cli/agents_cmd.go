package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents [task-id]",
	Short: "List running agents",
	Long: `Lists all agents, optionally filtered by task. Shows live status by checking tmux sessions.

Examples:
  ox agents          # list all agents across all tasks
  ox agents 18       # list agents for task #18`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgents,
}

func runAgents(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	var registries []*agent.AgentRegistry

	if len(args) > 0 {
		// Filter by task
		reg, err := findRegistryByRef(mgr, args[0])
		if err != nil {
			return err
		}
		registries = []*agent.AgentRegistry{reg}
	} else {
		var err error
		registries, err = mgr.ListAllRegistries()
		if err != nil {
			return fmt.Errorf("list agents: %w", err)
		}
	}

	if len(registries) == 0 {
		fmt.Println("No agents running. Use 'ox spawn' to create one.")
		return nil
	}

	for _, reg := range registries {
		// Reconcile statuses with tmux
		mgr.ReconcileStatus(reg.TaskID)
		// Reload after reconciliation
		reg, _ = mgr.LoadRegistry(reg.TaskID)

		fmt.Printf("Task #%d: %s\n", reg.TaskSeq, reg.TaskTitle)

		if len(reg.Agents) == 0 {
			fmt.Println("  No agents")
			continue
		}

		for _, a := range reg.Agents {
			icon := statusIcon(a.Status)
			live := ""
			if a.Status == agent.StatusRunning && tmuxutil.HasSession(a.TmuxSession) {
				live = " (live)"
			}

			var duration time.Duration
			if !a.SpawnedAt.IsZero() {
				duration = time.Since(a.SpawnedAt).Truncate(time.Second)
				if a.FinishedAt != nil {
					duration = a.FinishedAt.Sub(a.SpawnedAt).Truncate(time.Second)
				}
			}

			model := a.Model
			if model == "" {
				model = "default"
			}

			fmt.Printf("  %s %-20s [%-8s] %-8s %-8s %s%s\n",
				icon, a.ID, a.Status, a.Persona, model, duration, live)
		}
		fmt.Println()
	}

	return nil
}

func statusIcon(status agent.AgentStatus) string {
	switch status {
	case agent.StatusRunning:
		return "●"
	case agent.StatusDone:
		return "✓"
	case agent.StatusFailed:
		return "✗"
	case agent.StatusKilled:
		return "⊘"
	case agent.StatusPending:
		return "○"
	case agent.StatusIdle:
		return "◐"
	default:
		return "?"
	}
}

// findRegistryByRef finds an agent registry by task ID or sequence number.
func findRegistryByRef(mgr *agent.Manager, ref string) (*agent.AgentRegistry, error) {
	// Try direct task ID first
	if reg, err := mgr.LoadRegistry(ref); err == nil {
		return reg, nil
	}

	// Search by task seq
	registries, err := mgr.ListAllRegistries()
	if err != nil {
		return nil, err
	}

	var seqNum int
	if _, err := fmt.Sscanf(ref, "%d", &seqNum); err == nil {
		for _, reg := range registries {
			if reg.TaskSeq == seqNum {
				return reg, nil
			}
		}
	}

	// Search by partial match on task ID
	for _, reg := range registries {
		if strings.HasPrefix(reg.TaskID, ref) {
			return reg, nil
		}
	}

	return nil, fmt.Errorf("no agents found for task %q", ref)
}

func init() {
	rootCmd.AddCommand(agentsCmd)
}
