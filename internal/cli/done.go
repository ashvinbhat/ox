package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yoke"
	"github.com/spf13/cobra"
)

var (
	doneKeep   bool
	doneReason string
)

var doneCmd = &cobra.Command{
	Use:   "done [task-id]",
	Short: "Complete task and cleanup workspace",
	Long: `Marks a task as done in yoke and cleans up the workspace.

This command:
1. Marks the task as done in yoke
2. Removes git worktrees
3. Removes the workspace directory

Use --keep to preserve the workspace files.

Examples:
  ox done 9
  ox done 9 --keep
  ox done 9 --reason "Shipped in PR #123"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDone,
}

func runDone(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Find workspace
	var ws *workspace.TaskWorkspace
	var err error

	if len(args) > 0 {
		ws, err = workspace.Open(cfg.Home, args[0])
	} else {
		// Try to find single active workspace
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

	fmt.Printf("Completing task #%d...\n", ws.TaskSeq)

	// Mark task as done in yoke
	yokeClient, err := yoke.NewClient()
	if err != nil {
		fmt.Printf("Warning: could not open yoke: %v\n", err)
	} else {
		defer yokeClient.Close()
		t, err := yokeClient.Get(fmt.Sprintf("%d", ws.TaskSeq))
		if err != nil {
			fmt.Printf("Warning: task not found in yoke: %v\n", err)
		} else {
			if err := yokeClient.UpdateStatus(t.ID, "done"); err != nil {
				fmt.Printf("Warning: failed to update task status: %v\n", err)
			} else {
				fmt.Println("Task marked as done in yoke")
			}
			if doneReason != "" {
				if err := yokeClient.UpdateOutcome(t.ID, doneReason); err != nil {
					fmt.Printf("Warning: failed to update outcome: %v\n", err)
				}
			}
		}
	}

	if doneKeep {
		fmt.Printf("Workspace kept: %s\n", ws.Path)
		return nil
	}

	// Remove worktrees
	for _, repoName := range ws.Repos {
		worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", ws.TaskSeq))
		repoPath := filepath.Join(cfg.Home, "repos", repoName)

		fmt.Printf("Removing worktree %s...\n", repoName)
		if err := gitutil.RemoveWorktree(repoPath, worktreePath); err != nil {
			fmt.Printf("Warning: failed to remove worktree: %v\n", err)
			// Try to remove directory anyway
			os.RemoveAll(worktreePath)
		}
	}

	// Remove workspace
	fmt.Printf("Removing workspace...\n")
	if err := os.RemoveAll(ws.Path); err != nil {
		return fmt.Errorf("remove workspace: %w", err)
	}

	fmt.Println("Done!")
	return nil
}

func init() {
	doneCmd.Flags().BoolVar(&doneKeep, "keep", false, "Keep workspace files")
	doneCmd.Flags().StringVar(&doneReason, "reason", "", "Completion reason/outcome")
	rootCmd.AddCommand(doneCmd)
}
