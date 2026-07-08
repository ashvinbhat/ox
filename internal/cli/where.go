package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
)

var whereRepo string

var whereCmd = &cobra.Command{
	Use:   "where <mission-id | task-ref>",
	Short: "Show where a mission's code and files live",
	Long: `Prints the mission directory, each repo's integration worktree (the merged
state — run and test from here), and every worker's worktree (in-flight work).

Scripting: --repo prints just that repo's integration path, so
  cd "$(ox where 124 --repo backend)"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()

		var m *mission.Mission
		var err error
		if isAllDigits(args[0]) {
			seq, _ := strconv.Atoi(args[0])
			m, err = mission.FindByYokeSeq(cfg.Home, seq)
			if err != nil {
				return fmt.Errorf("no open mission for task #%d", seq)
			}
		} else {
			m, err = mission.Open(cfg.Home, args[0])
			if err != nil {
				return err
			}
		}

		if whereRepo != "" {
			binding, ok := m.Repos[whereRepo]
			if !ok {
				return fmt.Errorf("repo %q not bound to %s", whereRepo, m.ID)
			}
			fmt.Println(binding.IntegrationWorktree)
			return nil
		}

		fmt.Printf("Mission %s — %s\n", m.ID, m.Goal)
		fmt.Printf("  dir:        %s\n", m.Dir())
		fmt.Printf("  plan:       %s/plan.md\n", m.Dir())

		if len(m.Repos) == 0 {
			fmt.Println("  repos:      (none bound yet)")
		}
		for name, b := range m.Repos {
			fmt.Printf("  %s integration (merged state — run/test here):\n    %s  [%s]\n",
				name, b.IntegrationWorktree, b.IntegrationBranch)
		}

		if reg, err := harness.LoadRegistry(m); err == nil && len(reg.Workers) > 0 {
			fmt.Println("  workers (in-flight, own branches):")
			for _, w := range reg.Workers {
				loc := w.WorktreePath
				if _, err := os.Stat(loc); err != nil {
					loc += "  (pruned)"
				}
				branch := w.BranchName
				if branch == "" {
					branch = "shared dir"
				}
				fmt.Printf("    %-28s %-8s %s  [%s]\n", w.ID, w.Status, loc, branch)
			}
		}
		return nil
	},
}

func init() {
	whereCmd.Flags().StringVar(&whereRepo, "repo", "", "Print only this repo's integration worktree path")
	rootCmd.AddCommand(whereCmd)
}
