package cli

import (
	"fmt"
	"sort"

	"github.com/ashvinbhat/ox/internal/personas"
	"github.com/spf13/cobra"
)

var personasCmd = &cobra.Command{
	Use:   "personas",
	Short: "List available personas",
	Long: `Lists all available personas with their descriptions and triggers.

Personas define an agent's mindset and model preset:
  captain  - Orchestrates, plans, delegates
  builder  - Implements, ships code
  explorer - Researches, investigates
  reviewer - Reviews, checks quality`,
	RunE: runPersonasList,
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

	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

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

func init() {
	rootCmd.AddCommand(personasCmd)
}
