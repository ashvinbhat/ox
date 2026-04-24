package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/spf13/cobra"
)

var mergeSkip []string

var mergeCmd = &cobra.Command{
	Use:   "merge [task-id]",
	Short: "Merge agent branches sequentially with build gates",
	Long: `Merges completed agent branches one at a time into an integration branch.

After each merge, runs the repo's build_command (if configured) to verify
the build still passes. Stops on first failure.

Agents are merged in dependency order (from the plan).

Examples:
  ox merge 18                      # merge all done agents for task #18
  ox merge 18 --skip auth-tests    # skip a specific agent
  ox merge                         # merge most recent task's agents`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMerge,
}

func runMerge(cmd *cobra.Command, args []string) error {
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
		registries, err := mgr.ListAllRegistries()
		if err != nil || len(registries) == 0 {
			return fmt.Errorf("no agents found")
		}
		latest := registries[0]
		for _, reg := range registries[1:] {
			if reg.CreatedAt.After(latest.CreatedAt) {
				latest = reg
			}
		}
		taskID = latest.TaskID
	}

	// Reconcile statuses first
	mgr.ReconcileStatus(taskID)

	// Build skip set
	skipSet := make(map[string]bool)
	for _, id := range mergeSkip {
		skipSet[strings.TrimSpace(id)] = true
	}

	reg, err := mgr.LoadRegistry(taskID)
	if err != nil {
		return err
	}

	fmt.Printf("🔀 Merging agents for task #%d: %s\n", reg.TaskSeq, reg.TaskTitle)
	if reg.IntegrationBranch != "" {
		fmt.Printf("   Into branch: %s\n", reg.IntegrationBranch)
	}
	fmt.Println()

	// Show what we're about to merge
	doneCount := 0
	for _, a := range reg.Agents {
		if a.BranchName == "" {
			continue
		}
		icon := "○"
		status := string(a.Status)
		if a.Status == agent.StatusDone || a.Status == agent.StatusKilled {
			if skipSet[a.ID] {
				icon = "⊘"
				status = "skip"
			} else {
				icon = "●"
				status = "merge"
				doneCount++
			}
		}
		fmt.Printf("  %s %-20s [%s] %s\n", icon, a.ID, status, a.BranchName)
	}

	if doneCount == 0 {
		fmt.Println("\nNo agents ready to merge.")
		return nil
	}

	fmt.Printf("\nMerging %d agent(s)...\n", doneCount)

	report, err := agent.MergePipeline(mgr, cfg, taskID, skipSet)
	if report != nil {
		agent.DisplayReport(report)
	}
	if err != nil {
		return err
	}

	if report.TotalMerged > 0 {
		fmt.Println("\nNext steps:")
		fmt.Println("  ox ship    # push and create PR")
	}

	return nil
}

func init() {
	mergeCmd.Flags().StringSliceVar(&mergeSkip, "skip", nil, "Agent IDs to skip")
	rootCmd.AddCommand(mergeCmd)
}
