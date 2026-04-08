package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	reviewLocalFlag bool
	reviewDirFlag   string
)

var reviewCmd = &cobra.Command{
	Use:   "review [task-id] [repo]",
	Short: "Review local changes with Mycroft",
	Long: `Reviews uncommitted changes using Mycroft AI.

Can review:
- Current directory (any git repo)
- Specific directory
- Ox workspace by task ID

Examples:
  ox review              # Review current directory or workspace
  ox review --local      # Review current git directory (no workspace needed)
  ox review --dir ~/code/myproject  # Review specific directory
  ox review 9            # Review workspace for task #9
  ox review 9 backend    # Review specific repo in task #9 workspace`,
	Args: cobra.MaximumNArgs(2),
	RunE: runReview,
}

func runReview(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Handle --dir flag - review specific directory
	if reviewDirFlag != "" {
		return reviewDirectory(reviewDirFlag)
	}

	// Handle --local flag - review current directory
	if reviewLocalFlag {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		return reviewDirectory(cwd)
	}

	// Try workspace-based review
	var ws *workspace.TaskWorkspace
	var repoFilter string
	var err error

	// Parse arguments: could be [task-id], [repo], or [task-id] [repo]
	if len(args) == 0 {
		// No args - try current workspace, fall back to current directory
		ws, err = getCurrentWorkspace(cfg.Home)
		if err != nil {
			// Not in a workspace - review current directory
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			return reviewDirectory(cwd)
		}
	} else if len(args) == 1 {
		// One arg - could be task-id or repo name
		// Try as task-id first
		ws, err = workspace.Open(cfg.Home, args[0])
		if err != nil {
			// Not a task-id, try as repo in current workspace
			ws, err = getCurrentWorkspace(cfg.Home)
			if err != nil {
				return fmt.Errorf("not in a workspace and %q is not a valid task ID", args[0])
			}
			repoFilter = args[0]
		}
	} else {
		// Two args - task-id and repo
		ws, err = workspace.Open(cfg.Home, args[0])
		if err != nil {
			return fmt.Errorf("workspace not found for task %s: %w", args[0], err)
		}
		repoFilter = args[1]
	}

	// Determine which repos to review
	var reposToReview []string
	if repoFilter != "" {
		found := false
		for _, r := range ws.Repos {
			if r == repoFilter {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repo %q not in workspace (available: %v)", repoFilter, ws.Repos)
		}
		reposToReview = []string{repoFilter}
	} else {
		reposToReview = ws.Repos
	}

	// Review each repo
	for _, repo := range reposToReview {
		repoPath := filepath.Join(ws.Path, repo)

		// Resolve symlink to get actual worktree path
		resolved, err := filepath.EvalSymlinks(repoPath)
		if err != nil {
			fmt.Printf("Warning: could not resolve %s: %v\n", repo, err)
			resolved = repoPath
		}

		if err := reviewDirectory(resolved); err != nil {
			return fmt.Errorf("review failed for %s: %w", repo, err)
		}
	}

	return nil
}

func reviewDirectory(dir string) error {
	// Check if it's a git repo
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("%s is not a git repository", dir)
	}

	fmt.Printf("🔍 Reviewing %s...\n", dir)

	// Run mycroft review --local
	reviewCmd := exec.Command("mycroft", "review", "--local")
	reviewCmd.Dir = dir
	reviewCmd.Stdout = os.Stdout
	reviewCmd.Stderr = os.Stderr
	reviewCmd.Stdin = os.Stdin

	return reviewCmd.Run()
}

func init() {
	reviewCmd.Flags().BoolVar(&reviewLocalFlag, "local", false, "Review current directory (no workspace needed)")
	reviewCmd.Flags().StringVar(&reviewDirFlag, "dir", "", "Review specific directory")
	rootCmd.AddCommand(reviewCmd)
}
