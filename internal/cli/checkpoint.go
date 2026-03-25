package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/checkpoint"
	"github.com/ashvinbhat/ox/internal/yokehelper"
	"github.com/spf13/cobra"
)

var (
	checkpointDone      string
	checkpointNext      string
	checkpointDecisions []string
	checkpointSync      bool
)

var checkpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Save progress checkpoint",
	Long: `Saves a checkpoint of current progress for the active task.

Checkpoints capture:
- What was completed (--done)
- What's next (--next)
- Key decisions made (--decision)
- Files changed since last checkpoint

Examples:
  ox checkpoint --done "Implemented auth flow" --next "Add unit tests"
  ox checkpoint --done "Fixed bug" --decision "Using retry with backoff"
  ox checkpoint --done "Research complete" --sync   # Also sync to yoke notes`,
	RunE: runCheckpoint,
}

var checkpointsCmd = &cobra.Command{
	Use:   "checkpoints",
	Short: "List checkpoints for current task",
	Long:  `Lists all checkpoints for the current workspace, newest first.`,
	RunE:  runCheckpointsList,
}

var resumeCmd = &cobra.Command{
	Use:   "resume [checkpoint-id]",
	Short: "Show checkpoint context for resuming work",
	Long: `Displays checkpoint information to help resume work after a break.

Without an ID, shows the latest checkpoint.

Examples:
  ox resume                    # Show latest checkpoint
  ox resume 20240325-153000    # Show specific checkpoint`,
	RunE: runResume,
}

func runCheckpoint(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Get current workspace
	ws, err := getCurrentWorkspace(cfg.Home)
	if err != nil {
		return fmt.Errorf("not in a workspace: %w", err)
	}

	// Validate we have something to checkpoint
	if checkpointDone == "" && checkpointNext == "" {
		return fmt.Errorf("provide at least --done or --next")
	}

	// Create checkpoint
	mgr := checkpoint.NewManager(ws.Path, fmt.Sprintf("%d", ws.TaskSeq))
	cp, err := mgr.Create(checkpointDone, checkpointNext, checkpointDecisions)
	if err != nil {
		return fmt.Errorf("create checkpoint: %w", err)
	}

	fmt.Printf("Checkpoint saved: %s\n", cp.ID)
	if cp.Done != "" {
		fmt.Printf("  Done: %s\n", cp.Done)
	}
	if cp.Next != "" {
		fmt.Printf("  Next: %s\n", cp.Next)
	}
	if len(cp.FilesChanged) > 0 {
		fmt.Printf("  Files changed: %d\n", len(cp.FilesChanged))
	}
	if len(cp.Decisions) > 0 {
		fmt.Printf("  Decisions: %d\n", len(cp.Decisions))
	}

	// Sync to yoke notes if requested
	if checkpointSync {
		if err := syncCheckpointToYoke(ws.TaskSeq, cp); err != nil {
			fmt.Printf("Warning: failed to sync to yoke: %v\n", err)
		} else {
			fmt.Println("  Synced to yoke notes")
		}
	}

	return nil
}

func runCheckpointsList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Get current workspace
	ws, err := getCurrentWorkspace(cfg.Home)
	if err != nil {
		return fmt.Errorf("not in a workspace: %w", err)
	}

	mgr := checkpoint.NewManager(ws.Path, fmt.Sprintf("%d", ws.TaskSeq))
	checkpoints, err := mgr.List()
	if err != nil {
		return fmt.Errorf("list checkpoints: %w", err)
	}

	if len(checkpoints) == 0 {
		fmt.Println("No checkpoints yet.")
		fmt.Println("Use 'ox checkpoint --done \"...\" --next \"...\"' to create one.")
		return nil
	}

	fmt.Printf("Checkpoints for task #%d:\n\n", ws.TaskSeq)
	for _, cp := range checkpoints {
		fmt.Printf("  %s  %s\n", cp.ID, cp.CreatedAt.Format("2006-01-02 15:04"))
		if cp.Done != "" {
			// Truncate long done messages
			done := cp.Done
			if len(done) > 60 {
				done = done[:57] + "..."
			}
			fmt.Printf("    Done: %s\n", done)
		}
		if cp.Next != "" {
			next := cp.Next
			if len(next) > 60 {
				next = next[:57] + "..."
			}
			fmt.Printf("    Next: %s\n", next)
		}
		fmt.Println()
	}

	return nil
}

func runResume(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Get current workspace
	ws, err := getCurrentWorkspace(cfg.Home)
	if err != nil {
		return fmt.Errorf("not in a workspace: %w", err)
	}

	mgr := checkpoint.NewManager(ws.Path, fmt.Sprintf("%d", ws.TaskSeq))

	var cp *checkpoint.Checkpoint
	if len(args) > 0 {
		cp, err = mgr.Get(args[0])
		if err != nil {
			return fmt.Errorf("checkpoint not found: %w", err)
		}
	} else {
		cp, err = mgr.Latest()
		if err != nil {
			return fmt.Errorf("get latest checkpoint: %w", err)
		}
		if cp == nil {
			fmt.Println("No checkpoints found.")
			fmt.Println("Use 'ox checkpoint --done \"...\" --next \"...\"' to create one.")
			return nil
		}
	}

	// Display checkpoint in a format useful for resuming
	fmt.Printf("# Resume Context for Task #%d\n\n", ws.TaskSeq)
	fmt.Printf("Last checkpoint: %s (%s)\n\n", cp.ID, cp.CreatedAt.Format("2006-01-02 15:04"))

	if cp.Done != "" {
		fmt.Printf("## Completed\n%s\n\n", cp.Done)
	}

	if cp.Next != "" {
		fmt.Printf("## Next Steps\n%s\n\n", cp.Next)
	}

	if len(cp.Decisions) > 0 {
		fmt.Println("## Key Decisions")
		for _, d := range cp.Decisions {
			fmt.Printf("- %s\n", d)
		}
		fmt.Println()
	}

	if len(cp.FilesChanged) > 0 {
		fmt.Println("## Files Changed")
		// Group by repo
		byRepo := make(map[string][]string)
		for _, f := range cp.FilesChanged {
			parts := strings.SplitN(f, "/", 2)
			if len(parts) == 2 {
				byRepo[parts[0]] = append(byRepo[parts[0]], parts[1])
			} else {
				byRepo[""] = append(byRepo[""], f)
			}
		}
		for repo, files := range byRepo {
			if repo != "" {
				fmt.Printf("### %s\n", repo)
			}
			for _, f := range files {
				fmt.Printf("- %s\n", f)
			}
		}
		fmt.Println()
	}

	if len(cp.Blockers) > 0 {
		fmt.Println("## Blockers")
		for _, b := range cp.Blockers {
			fmt.Printf("- %s\n", b)
		}
		fmt.Println()
	}

	return nil
}

// syncCheckpointToYoke adds the checkpoint as a note in yoke.
func syncCheckpointToYoke(taskSeq int, cp *checkpoint.Checkpoint) error {
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return err
	}
	defer yokeClient.Close()

	taskRef := fmt.Sprintf("%d", taskSeq)
	t, err := yokeClient.Get(taskRef)
	if err != nil {
		return err
	}

	note := cp.ToYokeNote()
	return yokeClient.AddNote(t.ID, note)
}

func init() {
	checkpointCmd.Flags().StringVar(&checkpointDone, "done", "", "What was completed")
	checkpointCmd.Flags().StringVar(&checkpointNext, "next", "", "What's next")
	checkpointCmd.Flags().StringSliceVar(&checkpointDecisions, "decision", nil, "Key decision made (repeatable)")
	checkpointCmd.Flags().BoolVar(&checkpointSync, "sync", false, "Sync checkpoint to yoke notes")

	rootCmd.AddCommand(checkpointCmd)
	rootCmd.AddCommand(checkpointsCmd)
	rootCmd.AddCommand(resumeCmd)
}
