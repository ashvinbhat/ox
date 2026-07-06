package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

var missionsCmd = &cobra.Command{
	Use:   "missions",
	Short: "List and maintain missions",
	RunE:  runMissionsList,
}

var missionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List missions",
	RunE:  runMissionsList,
}

var missionsAll bool

func runMissionsList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	missions, err := mission.List(cfg.Home)
	if err != nil {
		return err
	}
	if len(missions) == 0 {
		fmt.Println("No missions. Start one: ox go <task-ref>")
		return nil
	}

	fmt.Printf("%-5s %-9s %-11s %-8s %-9s %s\n", "ID", "TYPE", "PHASE", "TASK", "SPENT", "GOAL")
	fmt.Println(strings.Repeat("-", 90))
	for _, m := range missions {
		if !missionsAll && !m.Open() {
			continue
		}
		task := "-"
		if m.Yoke != nil {
			task = fmt.Sprintf("#%d", m.Yoke.Seq)
		}
		goal := m.Goal
		if len(goal) > 44 {
			goal = goal[:41] + "..."
		}
		fmt.Printf("%-5s %-9s %-11s %-8s $%-8.2f %s\n", m.ID, m.Type, m.Phase, task, m.SpentUSD, goal)
	}
	return nil
}

var missionsDistillCmd = &cobra.Command{
	Use:   "distill <mission-id>",
	Short: "Run (or re-run) the distiller for a mission",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		m, err := mission.Open(cfg.Home, args[0])
		if err != nil {
			return err
		}
		fmt.Printf("Distilling %s (%s)...\n", m.ID, m.Goal)
		if err := harness.RunDistiller(cfg, m); err != nil {
			return err
		}
		fmt.Printf("Done — summary: %s/summary.md\n", m.Dir())
		return nil
	},
}

var missionsPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Clean up tmux sessions and worker worktrees of closed missions",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		missions, err := mission.List(cfg.Home)
		if err != nil {
			return err
		}

		open := map[string]bool{}
		for _, m := range missions {
			if m.Open() {
				open[m.TmuxSession()] = true
			}
		}

		sessions, _ := tmuxutil.ListSessions("ox-m")
		for _, s := range sessions {
			// Worker sessions belong to their mission session prefix.
			owner := s
			if idx := strings.Index(s[3:], "-"); idx >= 0 {
				owner = s[:3+idx]
			}
			if !open[owner] && !open[s] {
				fmt.Printf("Killing zombie session %s\n", s)
				tmuxutil.KillSession(s)
			}
		}

		for _, m := range missions {
			if m.Open() {
				continue
			}
			if reg, err := harness.LoadRegistry(m); err == nil {
				for _, w := range reg.Workers {
					if _, err := os.Stat(w.WorktreePath); err == nil {
						fmt.Printf("Removing worker worktree %s\n", w.WorktreePath)
					}
					harness.RemoveWorkerWorktree(cfg, w)
				}
			}
			for _, repo := range harness.PruneIntegration(cfg, m) {
				fmt.Printf("Removed integration worktree for %s (%s — PRs merged/closed)\n", repo, m.ID)
			}
		}
		fmt.Println("Prune complete")
		return nil
	},
}

func init() {
	missionsCmd.PersistentFlags().BoolVarP(&missionsAll, "all", "a", false, "Include closed missions")
	missionsCmd.AddCommand(missionsListCmd, missionsDistillCmd, missionsPruneCmd)
	rootCmd.AddCommand(missionsCmd)
}
