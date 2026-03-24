package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yoke"
	"github.com/spf13/cobra"
)

var (
	shipRepo   string
	shipDraft  bool
	shipDryRun bool
)

var shipCmd = &cobra.Command{
	Use:   "ship [task-id]",
	Short: "Push branches and create PRs",
	Long: `Pushes task branches to remote and creates pull requests.

This command:
1. Pushes all worktree branches to origin
2. Creates a PR for each repo using gh CLI
3. Links PRs in the output

Examples:
  ox ship              # Ship all repos in current workspace
  ox ship 9            # Ship specific task
  ox ship --repo backend   # Ship only backend repo
  ox ship --draft      # Create draft PRs`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShip,
}

func runShip(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Find workspace
	var ws *workspace.TaskWorkspace
	var err error

	if len(args) > 0 {
		ws, err = workspace.Open(cfg.Home, args[0])
	} else {
		workspaces, listErr := workspace.List(cfg.Home)
		if listErr != nil {
			return fmt.Errorf("list workspaces: %w", listErr)
		}
		if len(workspaces) == 0 {
			return fmt.Errorf("no active workspaces")
		}
		if len(workspaces) > 1 {
			return fmt.Errorf("multiple workspaces active, specify task ID")
		}
		ws = workspaces[0]
	}

	if err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	// Get task info for PR title/body
	var taskTitle string
	var taskSeq int
	yokeClient, err := yoke.NewClient()
	if err == nil {
		defer yokeClient.Close()
		if t, err := yokeClient.Get(fmt.Sprintf("%d", ws.TaskSeq)); err == nil {
			taskTitle = t.Title
			taskSeq = t.Seq
		}
	}
	if taskTitle == "" {
		taskTitle = ws.Slug
		taskSeq = ws.TaskSeq
	}

	// Find repos to ship
	reposToShip := findReposInWorkspace(ws.Path)
	if shipRepo != "" {
		// Filter to specific repo
		found := false
		for _, r := range reposToShip {
			if r == shipRepo {
				reposToShip = []string{shipRepo}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repo %q not in workspace", shipRepo)
		}
	}

	if len(reposToShip) == 0 {
		return fmt.Errorf("no repos found in workspace")
	}

	fmt.Printf("Shipping task #%d: %s\n\n", taskSeq, taskTitle)

	var prs []string

	for _, repoName := range reposToShip {
		worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", ws.TaskSeq))

		// Check if there are commits to push
		branch, err := gitutil.CurrentBranch(worktreePath)
		if err != nil {
			fmt.Printf("Warning: could not get branch for %s: %v\n", repoName, err)
			continue
		}

		fmt.Printf("=== %s (%s) ===\n", repoName, branch)

		if shipDryRun {
			fmt.Printf("  Would push %s to origin\n", branch)
			fmt.Printf("  Would create PR\n\n")
			continue
		}

		// Push branch
		fmt.Printf("  Pushing %s...\n", branch)
		if err := gitutil.Push(worktreePath, branch); err != nil {
			fmt.Printf("  Warning: push failed: %v\n", err)
			continue
		}

		// Create PR using gh CLI
		prURL, err := createPR(worktreePath, repoName, taskSeq, taskTitle, shipDraft)
		if err != nil {
			fmt.Printf("  Warning: PR creation failed: %v\n", err)
			continue
		}

		fmt.Printf("  PR: %s\n\n", prURL)
		prs = append(prs, fmt.Sprintf("%s: %s", repoName, prURL))
	}

	if len(prs) > 0 && !shipDryRun {
		fmt.Println("Created PRs:")
		for _, pr := range prs {
			fmt.Printf("  %s\n", pr)
		}
	}

	return nil
}

// findReposInWorkspace returns repo names that have symlinks in the workspace.
func findReposInWorkspace(workspacePath string) []string {
	var repos []string
	entries, err := os.ReadDir(workspacePath)
	if err != nil {
		return repos
	}

	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			repos = append(repos, e.Name())
		}
	}
	return repos
}

// createPR creates a pull request using gh CLI.
func createPR(worktreePath, repoName string, taskSeq int, taskTitle string, draft bool) (string, error) {
	title := fmt.Sprintf("#%d: %s", taskSeq, taskTitle)
	body := fmt.Sprintf("## Summary\nTask #%d: %s\n\n---\nShipped via `ox ship`", taskSeq, taskTitle)

	args := []string{"pr", "create", "--title", title, "--body", body}
	if draft {
		args = append(args, "--draft")
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if PR already exists
		if strings.Contains(string(output), "already exists") {
			// Get existing PR URL
			return getExistingPR(worktreePath)
		}
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}

	return strings.TrimSpace(string(output)), nil
}

// getExistingPR returns the URL of an existing PR for the current branch.
func getExistingPR(worktreePath string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "url", "-q", ".url")
	cmd.Dir = worktreePath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func init() {
	shipCmd.Flags().StringVarP(&shipRepo, "repo", "r", "", "Ship only this repo")
	shipCmd.Flags().BoolVar(&shipDraft, "draft", false, "Create draft PRs")
	shipCmd.Flags().BoolVar(&shipDryRun, "dry-run", false, "Show what would be done")

	rootCmd.AddCommand(shipCmd)
}
