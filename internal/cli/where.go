package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
		printMissionFiles(m)

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

// whereInfra is harness plumbing that would drown the actual deliverables.
var whereInfra = map[string]bool{
	"AGENTS.md": true, "orchestrator-prompt.md": true, "mission.yaml": true,
	"events.jsonl": true, "ledger.jsonl": true, "agents.json": true,
	"jobs.json": true, "watcher-state.json": true, "hook-cursor": true,
}

// printMissionFiles lists what the mission has produced — top-level artifacts
// (plan, findings, decisions, anything ad-hoc) plus worker outputs, newest
// first, so "where is the doc it wrote" has a one-command answer.
func printMissionFiles(m *mission.Mission) {
	type artifact struct {
		name string
		mod  time.Time
		size int64
	}
	var files []artifact

	entries, err := os.ReadDir(m.Dir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || whereInfra[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := e.Info(); err == nil {
			files = append(files, artifact{e.Name(), info.ModTime(), info.Size()})
		}
	}
	if reg, err := harness.LoadRegistry(m); err == nil {
		for id := range reg.Workers {
			rel := filepath.Join("workers", id, "output.md")
			if info, err := os.Stat(filepath.Join(m.Dir(), rel)); err == nil {
				files = append(files, artifact{rel, info.ModTime(), info.Size()})
			}
		}
	}
	if len(files) == 0 {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	fmt.Println("  files (newest first):")
	for _, f := range files {
		fmt.Printf("    %-40s %7s   %s\n", f.name, humanSize(f.size), humanAge(f.mod))
	}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
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
