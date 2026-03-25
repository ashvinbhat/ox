package cli

import (
	"fmt"

	"github.com/ashvinbhat/ox/internal/context"
	"github.com/ashvinbhat/ox/internal/yokehelper"
	"github.com/spf13/cobra"
)

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Regenerate AGENTS.md for current workspace",
	Long: `Regenerates the AGENTS.md file for the current workspace.

Use this after:
- Injecting or ejecting skills
- Changing personas with 'ox morph'
- Updating task notes in yoke

Examples:
  ox refresh`,
	RunE: runRefresh,
}

func runRefresh(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Get current workspace
	ws, err := getCurrentWorkspace(cfg.Home)
	if err != nil {
		return fmt.Errorf("not in a workspace: %w", err)
	}

	// Load yoke task
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return fmt.Errorf("open yoke: %w", err)
	}
	defer yokeClient.Close()

	t, err := yokeClient.Get(fmt.Sprintf("%d", ws.TaskSeq))
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// Load context
	notes, _ := yokeClient.GetNotes(t.ID)
	events, _ := yokeClient.GetEvents(t.ID)
	parent, _ := yokeClient.GetParent(t)
	children, _ := yokeClient.GetChildren(t.ID)
	blockers, _ := yokeClient.GetBlockers(t)

	gen := context.NewGenerator(cfg.Home)
	taskCtx := &context.TaskContext{
		Task:     t,
		Notes:    notes,
		Events:   events,
		Parent:   parent,
		Children: children,
		Blockers: blockers,
		Persona:  ws.Persona,
		Repos:    ws.Repos,
	}

	if err := gen.Generate(ws.Path, taskCtx); err != nil {
		return fmt.Errorf("generate AGENTS.md: %w", err)
	}

	fmt.Println("AGENTS.md regenerated")
	fmt.Printf("  Workspace: %s\n", ws.Path)
	fmt.Printf("  Persona: %s\n", ws.Persona)

	return nil
}

func init() {
	rootCmd.AddCommand(refreshCmd)
}
