package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage git worktrees",
	Long:  `List and manage git worktrees created by ox.`,
}

var worktreeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all worktrees",
	Long: `Lists all git worktrees managed by ox.

Shows worktrees organized by repo and task.`,
	RunE: runWorktreeList,
}

func runWorktreeList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	worktreesDir := filepath.Join(cfg.Home, "worktrees")
	if _, err := os.Stat(worktreesDir); os.IsNotExist(err) {
		fmt.Println("No worktrees")
		return nil
	}

	// List repos in worktrees dir
	repoEntries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return fmt.Errorf("read worktrees dir: %w", err)
	}

	if len(repoEntries) == 0 {
		fmt.Println("No worktrees")
		return nil
	}

	fmt.Printf("%-15s %-8s %-35s %s\n", "REPO", "TASK", "BRANCH", "PATH")
	fmt.Println(strings.Repeat("-", 90))

	for _, repoEntry := range repoEntries {
		if !repoEntry.IsDir() {
			continue
		}
		repoName := repoEntry.Name()
		repoWorktreesDir := filepath.Join(worktreesDir, repoName)

		taskEntries, err := os.ReadDir(repoWorktreesDir)
		if err != nil {
			continue
		}

		for _, taskEntry := range taskEntries {
			if !taskEntry.IsDir() {
				continue
			}
			taskID := taskEntry.Name()
			worktreePath := filepath.Join(repoWorktreesDir, taskID)

			// Get branch name
			branch, err := gitutil.CurrentBranch(worktreePath)
			if err != nil {
				branch = "(unknown)"
			}

			fmt.Printf("%-15s %-8s %-35s %s\n", repoName, "#"+taskID, branch, worktreePath)
		}
	}

	return nil
}

var (
	worktreeAddBranch string
)

var worktreeAddCmd = &cobra.Command{
	Use:   "add <repo>",
	Short: "Add a worktree to current task",
	Long: `Adds a worktree for an additional repo to the current task workspace.

Use this when you need to work on another repo for the same task.

Examples:
  ox worktree add frontend                           # New branch from main
  ox worktree add backend --branch feat/my-feature   # Use existing branch`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktreeAdd,
}

func runWorktreeAdd(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	repoName := args[0]

	// Verify repo exists
	rc, exists := cfg.Repos[repoName]
	if !exists {
		return fmt.Errorf("repo %q not registered", repoName)
	}

	// Find current workspace
	workspaces, err := workspace.List(cfg.Home)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("no active workspaces")
	}
	if len(workspaces) > 1 {
		return fmt.Errorf("multiple workspaces active, use 'ox pickup' with --repos flag instead")
	}

	ws := workspaces[0]

	// Check if worktree already exists
	worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", ws.TaskSeq))
	if _, err := os.Stat(worktreePath); err == nil {
		fmt.Printf("Worktree already exists: %s\n", worktreePath)
		return nil
	}

	// Create worktree
	repoPath := filepath.Join(cfg.Home, "repos", repoName)

	// Ensure parent dir exists
	os.MkdirAll(filepath.Dir(worktreePath), 0o755)

	// Fetch latest
	fmt.Printf("Fetching %s...\n", repoName)
	if err := gitutil.Fetch(repoPath); err != nil {
		fmt.Printf("Warning: fetch failed: %v\n", err)
	}

	if worktreeAddBranch != "" {
		// Use existing branch
		remoteBranch := worktreeAddBranch
		if !strings.HasPrefix(remoteBranch, "origin/") {
			remoteBranch = "origin/" + remoteBranch
		}

		fmt.Printf("Creating worktree from existing branch %s...\n", remoteBranch)
		// Create worktree tracking the remote branch
		if err := gitutil.CreateWorktreeFromRemoteBranch(repoPath, worktreePath, worktreeAddBranch, remoteBranch); err != nil {
			return fmt.Errorf("create worktree: %w", err)
		}
	} else {
		// Create new branch from base
		branchName := fmt.Sprintf("ox/%d-%s", ws.TaskSeq, slugify(ws.Slug))
		baseBranch := rc.BaseBranch
		if baseBranch == "" {
			baseBranch = "origin/main"
		}

		fmt.Printf("Creating worktree %s from %s...\n", branchName, baseBranch)
		if err := gitutil.CreateWorktreeFromRef(repoPath, worktreePath, branchName, baseBranch); err != nil {
			return fmt.Errorf("create worktree: %w", err)
		}
	}

	// Copy files from repo to worktree (e.g., .env, .vscode/)
	if len(rc.CopyFiles) > 0 {
		for _, file := range rc.CopyFiles {
			src := filepath.Join(repoPath, file)
			dst := filepath.Join(worktreePath, file)
			if err := copyPath(src, dst); err != nil {
				fmt.Printf("Warning: failed to copy %s: %v\n", file, err)
			} else {
				fmt.Printf("  Copied %s\n", file)
			}
		}
	}

	// Run post-setup command if specified
	if rc.PostSetup != "" {
		fmt.Printf("Running post-setup: %s\n", rc.PostSetup)
		cmd := exec.Command("sh", "-c", rc.PostSetup)
		cmd.Dir = worktreePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: post-setup failed: %v\n", err)
		}
	}

	// Symlink into workspace
	linkPath := filepath.Join(ws.Path, repoName)
	os.Remove(linkPath) // Remove if exists
	if err := os.Symlink(worktreePath, linkPath); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	fmt.Printf("Added %s to workspace\n", repoName)
	return nil
}

var worktreeRmCmd = &cobra.Command{
	Use:   "rm <repo>",
	Short: "Remove a worktree from current task",
	Long: `Removes a worktree from the current task workspace.

Example:
  ox worktree rm frontend   # Remove frontend worktree`,
	Args: cobra.ExactArgs(1),
	RunE: runWorktreeRm,
}

func runWorktreeRm(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	repoName := args[0]

	// Find current workspace
	workspaces, err := workspace.List(cfg.Home)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("no active workspaces")
	}
	if len(workspaces) > 1 {
		return fmt.Errorf("multiple workspaces active")
	}

	ws := workspaces[0]
	worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", ws.TaskSeq))
	repoPath := filepath.Join(cfg.Home, "repos", repoName)

	// Remove symlink from workspace
	linkPath := filepath.Join(ws.Path, repoName)
	os.Remove(linkPath)

	// Remove worktree
	fmt.Printf("Removing worktree %s...\n", repoName)
	if err := gitutil.RemoveWorktree(repoPath, worktreePath); err != nil {
		// Try to remove directory anyway
		os.RemoveAll(worktreePath)
	}

	fmt.Printf("Removed %s from workspace\n", repoName)
	return nil
}

func init() {
	worktreeAddCmd.Flags().StringVar(&worktreeAddBranch, "branch", "", "Use existing remote branch instead of creating new")

	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeAddCmd)
	worktreeCmd.AddCommand(worktreeRmCmd)
	rootCmd.AddCommand(worktreeCmd)
}
