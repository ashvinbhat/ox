package cli

import (
	"fmt"
	"os"
	"strconv"
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
			harness.PruneReviewWorktree(cfg, m)
		}
		fmt.Println("Prune complete")
		return nil
	},
}

func init() {
	missionsCmd.PersistentFlags().BoolVarP(&missionsAll, "all", "a", false, "Include closed missions")
	missionsCloseCmd.Flags().StringVar(&closeOutcome, "outcome", "", "Outcome to record")
	missionsCmd.AddCommand(missionsListCmd, missionsDistillCmd, missionsPruneCmd, missionsCloseCmd)
	rootCmd.AddCommand(missionsCmd)
}

var closeOutcome string

var missionsCloseCmd = &cobra.Command{
	Use:   "close <mission-id | task-ref>",
	Short: "Close a mission from outside (no live orchestrator needed)",
	Long: `The same close path the orchestrator uses — refuses while workers run,
records the outcome, then distills memories and reaps worktrees/sessions.
For when the orchestrator session is gone or you just want it done.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		var m *mission.Mission
		var err error
		if isAllDigits(args[0]) {
			seq, _ := strconv.Atoi(args[0])
			m, err = mission.FindByYokeSeq(cfg.Home, seq)
		} else {
			m, err = mission.Open(cfg.Home, args[0])
		}
		if err != nil {
			return err
		}
		if !m.Open() {
			fmt.Printf("%s is already closed (%s)\n", m.ID, m.Outcome)
			return nil
		}
		if reg, err := harness.LoadRegistry(m); err == nil {
			for id, w := range reg.Workers {
				if w.Status == harness.WorkerRunning || w.Status == harness.WorkerBlocked {
					return fmt.Errorf("cannot close: worker %s is %s — kill it or let it finish first", id, w.Status)
				}
			}
		}
		outcome := closeOutcome
		if outcome == "" {
			outcome = "Closed by user via ox missions close"
		}
		if _, err := mission.Update(cfg.Home, m.ID, func(mm *mission.Mission) error {
			mm.Outcome = outcome
			mm.SetPhase(mission.PhaseClosed, "user")
			return nil
		}); err != nil {
			return err
		}
		m, _ = mission.Open(cfg.Home, m.ID)
		if err := harness.CloseMission(cfg, m); err != nil {
			fmt.Printf("warning: close cleanup: %v\n", err)
		}
		tmuxutil.KillSession(m.TmuxSession())
		fmt.Printf("%s closed: %s\n", m.ID, outcome)
		return nil
	},
}
