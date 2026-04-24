package cli

import (
	"fmt"
	"time"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/scratchpad"
	"github.com/spf13/cobra"
)

var (
	scratchKind  string
	scratchTask  string
	scratchRead  bool
	scratchSince string
)

var scratchCmd = &cobra.Command{
	Use:   "scratch [message]",
	Short: "Read or write to the shared agent scratchpad",
	Long: `A shared bulletin board for agent communication.

Agents can post discoveries, questions, decisions, and blockers.
All agents see the same scratchpad for their task.

Examples:
  ox scratch "DB uses snake_case columns" --kind discovery --task 18
  ox scratch "Should login support SSO?" --kind question --task 18
  ox scratch --read --task 18
  ox scratch --read --since 1h --task 18`,
	Args: cobra.MaximumNArgs(1),
	RunE: runScratch,
}

func runScratch(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	// Find task
	var taskID string
	if scratchTask != "" {
		reg, err := findRegistryByRef(mgr, scratchTask)
		if err != nil {
			return err
		}
		taskID = reg.TaskID
	} else {
		registries, err := mgr.ListAllRegistries()
		if err != nil || len(registries) == 0 {
			return fmt.Errorf("no agents found — specify --task")
		}
		latest := registries[0]
		for _, reg := range registries[1:] {
			if reg.CreatedAt.After(latest.CreatedAt) {
				latest = reg
			}
		}
		taskID = latest.TaskID
	}

	pad := scratchpad.New(mgr.AgentsDir(taskID))

	if scratchRead || len(args) == 0 {
		// Read mode
		if scratchSince != "" {
			dur, err := time.ParseDuration(scratchSince)
			if err != nil {
				return fmt.Errorf("invalid duration %q: %w", scratchSince, err)
			}
			content, err := pad.ReadSince(time.Now().Add(-dur))
			if err != nil {
				return err
			}
			fmt.Print(content)
		} else {
			content, err := pad.Read()
			if err != nil {
				return err
			}
			fmt.Print(content)
		}
		return nil
	}

	// Write mode
	message := args[0]
	agentID := "user"

	entry := scratchpad.Entry{
		AgentID: agentID,
		Kind:    scratchKind,
		Content: message,
	}

	if err := pad.Append(entry); err != nil {
		return err
	}

	fmt.Printf("Added to scratchpad (%s): %s\n", scratchKind, message)
	return nil
}

func init() {
	scratchCmd.Flags().StringVar(&scratchKind, "kind", "discovery", "Entry kind: discovery, question, decision, blocker")
	scratchCmd.Flags().StringVar(&scratchTask, "task", "", "Task ID or sequence number")
	scratchCmd.Flags().BoolVar(&scratchRead, "read", false, "Read the scratchpad")
	scratchCmd.Flags().StringVar(&scratchSince, "since", "", "Show entries since duration (e.g., 1h, 30m)")

	rootCmd.AddCommand(scratchCmd)
}
