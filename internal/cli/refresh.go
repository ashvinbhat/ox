package cli

import (
	"fmt"

	"github.com/ashvinbhat/ox/internal/context"
	"github.com/ashvinbhat/ox/internal/yokecli"
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

	taskRef := fmt.Sprintf("%d", ws.TaskSeq)
	t, err := yokecli.Get(taskRef)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	taskMD, err := yokecli.ContextMarkdown(taskRef)
	if err != nil {
		return fmt.Errorf("load task context: %w", err)
	}

	gen := context.NewGenerator(cfg.Home)
	taskCtx := &context.TaskContext{
		TaskMarkdown: taskMD,
		Title:        t.Title,
		Tags:         t.Tags,
		Persona:      ws.Persona,
		Repos:        ws.Repos,
	}

	if err := gen.Generate(ws.Path, taskCtx); err != nil {
		return fmt.Errorf("generate AGENTS.md: %w", err)
	}

	linkYokeDocs(ws.Path)

	fmt.Println("AGENTS.md regenerated")
	fmt.Printf("  Workspace: %s\n", ws.Path)
	fmt.Printf("  Persona: %s\n", ws.Persona)

	return nil
}

func init() {
	rootCmd.AddCommand(refreshCmd)
}
