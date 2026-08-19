package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
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

	printedWorkers := printMissionWorkers(cfg, args)

	var registries []*agent.AgentRegistry

	if len(args) > 0 {
		// Filter by task
		reg, err := findRegistryByRef(mgr, args[0])
		if err != nil {
			if printedWorkers {
				return nil
			}
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
		if !printedWorkers {
			fmt.Println("No agents running. Use 'ox go' to start a mission.")
		}
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

// printMissionWorkers lists harness mission workers; returns whether any
// mission section was printed.
func printMissionWorkers(cfg *config.Config, args []string) bool {
	missions, err := mission.List(cfg.Home)
	if err != nil {
		return false
	}

	printed := false
	for _, m := range missions {
		if !m.Open() {
			continue
		}
		if len(args) > 0 && args[0] != m.ID {
			continue
		}
		reg, err := harness.LoadRegistry(m)
		if err != nil || len(reg.Workers) == 0 {
			continue
		}

		fmt.Printf("Mission %s [%s/%s]: %s\n", m.ID, m.Type, m.Phase, m.Goal)
		ids := make([]string, 0, len(reg.Workers))
		for id := range reg.Workers {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			w := reg.Workers[id]
			live := ""
			if w.Status == harness.WorkerRunning && w.Alive() {
				live = " (live)"
			}
			duration := time.Since(w.SpawnedAt).Truncate(time.Second)
			if w.FinishedAt != nil {
				duration = w.FinishedAt.Sub(w.SpawnedAt).Truncate(time.Second)
			}
			fmt.Printf("  %s %-20s [%-11s] %-8s %-8s %s%s\n",
				workerIcon(w.Status), w.ID, w.Status, w.Persona, w.Model, duration, live)
		}
		fmt.Println()
		printed = true
	}
	return printed
}

func workerIcon(status string) string {
	switch status {
	case harness.WorkerRunning:
		return "●"
	case harness.WorkerDone:
		return "✓"
	case harness.WorkerFailed:
		return "✗"
	case harness.WorkerKilled:
		return "⊘"
	case harness.WorkerPending:
		return "○"
	case harness.WorkerBlocked:
		return "■"
	case harness.WorkerInterrupted:
		return "◌"
	default:
		return "?"
	}
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
