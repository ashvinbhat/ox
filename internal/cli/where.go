package cli

import (
	"fmt"
	"os"
	"os/exec"
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

		unmerged := 0
		if reg, err := harness.LoadRegistry(m); err == nil && len(reg.Workers) > 0 {
			fmt.Println("  workers:")
			for _, w := range reg.Workers {
				loc := w.WorktreePath
				if _, err := os.Stat(loc); err != nil {
					loc += "  (pruned)"
				}
				branch := w.BranchName
				merge := ""
				if branch == "" {
					branch = "shared dir"
				} else if b, ok := m.Repos[w.Repo]; ok {
					if branchMerged(b.IntegrationWorktree, w.BranchName) {
						merge = "  merged✓"
					} else {
						merge = "  NOT merged"
						unmerged++
					}
				}
				fmt.Printf("    %-28s %-8s %s  [%s]%s\n", w.ID, w.Status, loc, branch, merge)
			}
		}

		switch {
		case len(m.Repos) == 0:
		case unmerged > 0:
			fmt.Printf("\n  ⚠ %d worker branch(es) not merged yet — integration is BEHIND the latest\n", unmerged)
			fmt.Println("    work; test the worker worktree, or ask the orchestrator to merge first.")
		default:
			fmt.Println("\n  ✓ integration worktree is current — run and test there.")
		}
		return nil
	},
}

// branchMerged reports whether branch is an ancestor of the integration
// worktree's HEAD (i.e. its work is folded in).
func branchMerged(integrationWorktree, branch string) bool {
	if _, err := os.Stat(integrationWorktree); err != nil {
		return false
	}
	return exec.Command("git", "-C", integrationWorktree,
		"merge-base", "--is-ancestor", branch, "HEAD").Run() == nil
}

func init() {
	whereCmd.Flags().StringVar(&whereRepo, "repo", "", "Print only this repo's integration worktree path")
	rootCmd.AddCommand(whereCmd)
}
