package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/ashvinbhat/ox/internal/context"
	"github.com/ashvinbhat/ox/internal/personas"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokecli"
	"github.com/spf13/cobra"
)

var personasCmd = &cobra.Command{
	Use:   "personas",
	Short: "List available personas",
	Long: `Lists all available personas with their descriptions and triggers.

Personas define the AI agent's mindset and approach:
  captain  - Orchestrates, plans, delegates
  builder  - Implements, ships code
  explorer - Researches, investigates
  reviewer - Reviews, checks quality`,
	RunE: runPersonasList,
}

var morphCmd = &cobra.Command{
	Use:   "morph <persona>",
	Short: "Switch to a different persona",
	Long: `Switches the current workspace to a different persona.

This regenerates AGENTS.md with the new persona's context.

Examples:
  ox morph explorer   # Switch to research mode
  ox morph builder    # Switch to implementation mode`,
	Args: cobra.ExactArgs(1),
	RunE: runMorph,
}

func runPersonasList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	reg, err := personas.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load personas: %w", err)
	}

	all := reg.All()
	if len(all) == 0 {
		fmt.Println("No personas found.")
		fmt.Println("Run 'ox init' to create default personas.")
		return nil
	}

	// Sort by name
	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})

	fmt.Println("Available personas:")
	fmt.Println()
	for _, p := range all {
		fmt.Printf("  %-12s %s\n", p.Name, p.Role)
		if p.Description != "" {
			fmt.Printf("               %s\n", p.Description)
		}
		if len(p.Triggers) > 0 {
			fmt.Printf("               Triggers: %v\n", p.Triggers)
		}
		fmt.Println()
	}

	return nil
}

func runMorph(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	personaName := args[0]

	// Validate persona exists
	reg, err := personas.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load personas: %w", err)
	}

	persona, ok := reg.Get(personaName)
	if !ok {
		fmt.Printf("Persona %q not found.\n", personaName)
		fmt.Println("Available personas:")
		for _, name := range reg.List() {
			fmt.Printf("  - %s\n", name)
		}
		return fmt.Errorf("persona not found")
	}

	// Get current workspace
	ws, err := getCurrentWorkspace(cfg.Home)
	if err != nil {
		return fmt.Errorf("no active workspace: %w", err)
	}

	taskRef := fmt.Sprintf("%d", ws.TaskSeq)
	t, err := yokecli.Get(taskRef)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// Update workspace persona
	ws.Persona = personaName
	ws.TaskID = t.ID
	if err := ws.SaveState(); err != nil {
		fmt.Printf("Warning: failed to save workspace state: %v\n", err)
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
		Persona:      personaName,
		Repos:        ws.Repos,
	}

	if err := gen.Generate(ws.Path, taskCtx); err != nil {
		return fmt.Errorf("regenerate AGENTS.md: %w", err)
	}

	linkYokeDocs(ws.Path)

	fmt.Printf("Morphed to %s persona.\n", persona.Name)
	fmt.Printf("  Role: %s\n", persona.Role)
	fmt.Println("\nAGENTS.md regenerated with new persona.")

	return nil
}

// getCurrentWorkspace finds the workspace for the current directory.
func getCurrentWorkspace(oxHome string) (*workspace.TaskWorkspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// List all workspaces and find one matching current dir
	workspaces, err := workspace.List(oxHome)
	if err != nil {
		return nil, err
	}

	for _, ws := range workspaces {
		if cwd == ws.Path || isSubdir(ws.Path, cwd) {
			return ws, nil
		}
	}

	return nil, fmt.Errorf("not in a workspace directory")
}

// resolveWorkspace picks the workspace to operate on: the explicit ref if
// given, else the workspace containing the cwd, else the sole active one.
func resolveWorkspace(oxHome, ref string) (*workspace.TaskWorkspace, error) {
	if ref != "" {
		return workspace.Open(oxHome, ref)
	}
	if ws, err := getCurrentWorkspace(oxHome); err == nil {
		return ws, nil
	}
	workspaces, err := workspace.List(oxHome)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return nil, fmt.Errorf("no active workspaces")
	}
	if len(workspaces) > 1 {
		return nil, fmt.Errorf("multiple workspaces active: run from inside one or specify the task ID")
	}
	return workspaces[0], nil
}

// isSubdir checks if child is a subdirectory of parent.
func isSubdir(parent, child string) bool {
	rel, err := os.Readlink(child)
	if err == nil {
		// Check if symlink target is under parent
		return len(rel) > len(parent) && rel[:len(parent)] == parent
	}
	return len(child) > len(parent)+1 && child[:len(parent)+1] == parent+"/"
}

func init() {
	rootCmd.AddCommand(personasCmd)
	rootCmd.AddCommand(morphCmd)
}
