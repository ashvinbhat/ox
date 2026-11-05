package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/jobs"
	"github.com/ashvinbhat/ox/internal/worktrees"
)

var (
	pruneDryRun bool
	pruneIfDue  bool
	pruneJSON   bool
)

// The start hook calls prune --if-due at every thread start; once an hour is
// enough to catch merges without a gh call per thread.
const pruneEvery = time.Hour

var worktreesCmd = &cobra.Command{
	Use:   "worktrees",
	Short: "Manage git worktrees",
}

var worktreesPruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove worktrees whose PR has merged",
	Long: `Removes the git worktrees (and their local branches) whose pull request has
merged. A worktree is kept when an open desktop thread works in it, when it has
uncommitted or untracked files, or when its HEAD has commits the merged PR never
saw. Threads' own worktrees are left to the desktop app, which removes them when
it archives the thread.

Repos scanned: every clone in ~/.ox/repos plus the repo of every open thread.
Removals are logged to ~/.ox/logs/worktrees.log.`,
	Args: cobra.NoArgs,
	RunE: runWorktreesPrune,
}

func runWorktreesPrune(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	var outcomes []worktrees.Outcome
	prune := func() error {
		var err error
		outcomes, err = pruneWorktrees(cfg.Home, pruneDryRun)
		return err
	}
	if pruneIfDue {
		_, err := jobs.Run(cfg.Home, jobWorktrees, pruneEvery, pruneLimit, prune)
		return err
	}
	err := prune()
	if !pruneDryRun {
		jobs.Note(cfg.Home, jobWorktrees, err)
	}
	if err != nil {
		return err
	}
	if pruneJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(outcomes)
	}
	printPrune(outcomes, pruneDryRun)
	return nil
}

// pruneWorktrees removes what Prune clears and logs each removal, even on a
// run that also failed some lookups.
func pruneWorktrees(oxHome string, dryRun bool) ([]worktrees.Outcome, error) {
	sessions, _ := desktop.List(desktop.DefaultDir(), false)
	inUse := map[string]bool{}
	for _, s := range sessions {
		if s.Cwd != "" {
			inUse[filepath.Clean(s.Cwd)] = true
		}
	}
	if wd, err := os.Getwd(); err == nil {
		inUse[filepath.Clean(wd)] = true
	}
	outcomes, err := worktrees.Prune(knownRepos(oxHome, sessions), inUse, dryRun)
	if !dryRun {
		if mkErr := os.MkdirAll(filepath.Join(oxHome, "logs"), 0o755); mkErr == nil {
			logRemovals(filepath.Join(oxHome, "logs", "worktrees.log"), outcomes)
		}
	}
	return outcomes, err
}

// knownRepos is every clone in ~/.ox/repos plus the main checkout of every
// open thread's repo.
func knownRepos(oxHome string, sessions []desktop.Session) []string {
	set := map[string]bool{}
	for _, s := range sessions {
		if s.Cwd == "" {
			continue
		}
		if main, err := worktrees.MainCheckout(s.Cwd); err == nil {
			set[main] = true
		}
	}
	clones, _ := filepath.Glob(filepath.Join(oxHome, "repos", "*", ".git"))
	for _, g := range clones {
		set[filepath.Dir(g)] = true
	}
	repos := make([]string, 0, len(set))
	for r := range set {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	return repos
}

func printPrune(outcomes []worktrees.Outcome, dryRun bool) {
	var removed, kept []worktrees.Outcome
	for _, o := range outcomes {
		if o.Removed {
			removed = append(removed, o)
		} else {
			kept = append(kept, o)
		}
	}
	if len(outcomes) == 0 {
		fmt.Println("No worktrees with a merged PR.")
		return
	}
	verb := "Removed"
	if dryRun {
		verb = "Would remove"
	}
	if len(removed) > 0 {
		fmt.Printf("%s %d worktree(s) whose PR merged:\n", verb, len(removed))
		for _, o := range removed {
			fmt.Printf("  %-26s %-40s %s\n", prRef(filepath.Base(o.Repo), o.PR), clip(o.Branch, 40), o.Path)
		}
	}
	if len(kept) > 0 {
		fmt.Printf("Kept %d with a merged PR:\n", len(kept))
		for _, o := range kept {
			fmt.Printf("  %-26s %-40s %s\n", prRef(filepath.Base(o.Repo), o.PR), clip(o.Branch, 40), o.Reason)
		}
	}
}

func logRemovals(path string, outcomes []worktrees.Outcome) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	now := time.Now().Format(time.RFC3339)
	for _, o := range outcomes {
		if o.Removed {
			fmt.Fprintf(f, "%s removed %s (%s, PR #%d merged)\n", now, o.Path, o.Branch, o.PR)
		}
	}
}

func init() {
	worktreesPruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Show what would be removed")
	worktreesPruneCmd.Flags().BoolVar(&pruneIfDue, "if-due", false, "Skip unless the last good prune is an hour old (the start hook uses this)")
	worktreesPruneCmd.Flags().BoolVar(&pruneJSON, "json", false, "Machine-readable output")
	worktreesCmd.AddCommand(worktreesPruneCmd)
	rootCmd.AddCommand(worktreesCmd)
}
