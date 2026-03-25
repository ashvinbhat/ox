package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/ashvinbhat/ox/internal/context"
	"github.com/ashvinbhat/ox/internal/personas"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokehelper"
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

	fmt.Println("Available personas:\n")
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

	// Load task from yoke
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return fmt.Errorf("open yoke: %w", err)
	}
	defer yokeClient.Close()

	taskRef := fmt.Sprintf("%d", ws.TaskSeq)
	t, err := yokeClient.Get(taskRef)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// Update workspace persona
	ws.Persona = personaName

	// Regenerate AGENTS.md
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
		Persona:  personaName,
		Repos:    ws.Repos,
	}

	if err := gen.Generate(ws.Path, taskCtx); err != nil {
		return fmt.Errorf("regenerate AGENTS.md: %w", err)
	}

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
