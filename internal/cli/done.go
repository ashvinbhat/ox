package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/checkpoint"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokecli"
	"github.com/spf13/cobra"
)

var (
	doneKeep         bool
	doneReason       string
	doneNoCheckpoint bool
	doneLearn        string
)

var doneCmd = &cobra.Command{
	Use:   "done [task-id]",
	Short: "Complete task and cleanup workspace",
	Long: `Marks a task as done in yoke and cleans up the workspace.

This command:
1. Creates a final checkpoint (captures files changed)
2. Marks the task as done in yoke
3. Removes git worktrees
4. Removes the workspace directory

Use --keep to preserve the workspace files.
Use --no-checkpoint to skip the final checkpoint.

Examples:
  ox done 9
  ox done 9 --keep
  ox done 9 --reason "Shipped in PR #123"
  ox done 9 --learn "Always add index hints for MongoDB aggregations"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDone,
}

func runDone(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Find workspace (optional when a task ID is supplied — parent tasks
	// and externally-tracked work may have no ox workspace).
	var ws *workspace.TaskWorkspace
	var taskRef string

	if len(args) > 0 {
		taskRef = args[0]
		ws, _ = workspace.Open(cfg.Home, args[0])
	} else {
		var err error
		ws, err = resolveWorkspace(cfg.Home, "")
		if err != nil {
			return fmt.Errorf("%w (pass a task ID to mark a workspace-less task done)", err)
		}
		taskRef = fmt.Sprintf("%d", ws.TaskSeq)
	}

	if ws != nil {
		fmt.Printf("Completing task #%d...\n", ws.TaskSeq)
	} else {
		fmt.Printf("Completing task %s (no workspace — yoke-only)...\n", taskRef)
	}

	// Auto-checkpoint before cleanup (workspace-only, unless --no-checkpoint)
	if ws != nil && !doneNoCheckpoint {
		mgr := checkpoint.NewManager(ws.Path, fmt.Sprintf("%d", ws.TaskSeq))
		doneMsg := "Task completed"
		if doneReason != "" {
			doneMsg = doneReason
		}
		cp, err := mgr.Create(doneMsg, "", nil)
		if err != nil {
			fmt.Printf("Warning: failed to create final checkpoint: %v\n", err)
		} else {
			fmt.Printf("Final checkpoint saved: %s\n", cp.ID)
		}
	}

	// Mark task as done in yoke
	t, err := yokecli.Get(taskRef)
	if err != nil {
		fmt.Printf("Warning: task not found in yoke: %v\n", err)
	} else {
		if err := yokecli.Done(fmt.Sprintf("%d", t.Seq), doneReason); err != nil {
			fmt.Printf("Warning: failed to mark task done: %v\n", err)
		} else {
			fmt.Println("Task marked as done in yoke")
		}

		// Capture learning if provided (uses task scope even when no workspace)
		if doneLearn != "" {
			if store, lerr := openMemoryStore(); lerr != nil {
				fmt.Printf("Warning: failed to save memory: %v\n", lerr)
			} else {
				scope := fmt.Sprintf("task:%d", t.Seq)
				var tags []string
				if ws != nil && len(ws.Repos) > 0 {
					scope = "repo:" + ws.Repos[0]
					tags = ws.Repos
				}
				res, lerr := store.Remember(cmd.Context(), memory.RememberInput{
					Content: doneLearn, Kind: "learning", Scope: scope, Tags: tags, Source: "user",
				})
				store.Close()
				if lerr != nil {
					fmt.Printf("Warning: failed to save memory: %v\n", lerr)
				} else {
					fmt.Printf("Remembered (%s)\n", res.UID)
				}
			}
		}
	}

	if ws == nil {
		fmt.Println("Done!")
		return nil
	}

	if doneKeep {
		fmt.Printf("Workspace kept: %s\n", ws.Path)
		return nil
	}

	// Remove worktrees. Multi-agent runs create extra worktrees alongside the
	// plain "<seq>" path: "<seq>-integration" and "<seq>-<agent-id>". Match
	// "<seq>" exactly OR "<seq>-*" so all of them get cleaned up.
	for _, repoName := range ws.Repos {
		repoPath := filepath.Join(cfg.Home, "repos", repoName)
		repoWorktreesDir := filepath.Join(cfg.Home, "worktrees", repoName)
		seqStr := fmt.Sprintf("%d", ws.TaskSeq)

		entries, _ := os.ReadDir(repoWorktreesDir)
		matched := []string{}
		for _, e := range entries {
			name := e.Name()
			if name == seqStr || strings.HasPrefix(name, seqStr+"-") {
				matched = append(matched, filepath.Join(repoWorktreesDir, name))
			}
		}
		// Fallback: even if directory entry is gone, still try the canonical path
		// so git's worktree metadata gets cleaned up.
		if len(matched) == 0 {
			matched = append(matched, filepath.Join(repoWorktreesDir, seqStr))
		}

		for _, worktreePath := range matched {
			fmt.Printf("Removing worktree %s (%s)...\n", repoName, filepath.Base(worktreePath))
			if err := gitutil.RemoveWorktree(repoPath, worktreePath); err != nil {
				fmt.Printf("Warning: failed to remove worktree: %v\n", err)
				os.RemoveAll(worktreePath)
			}
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
	doneCmd.Flags().BoolVar(&doneNoCheckpoint, "no-checkpoint", false, "Skip final checkpoint")
	doneCmd.Flags().StringVar(&doneLearn, "learn", "", "Capture a learning from this task")
	rootCmd.AddCommand(doneCmd)
}
