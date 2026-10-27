package cli

import (
	"fmt"

	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/spf13/cobra"
)

var learningsCmd = &cobra.Command{
	Use:   "learnings",
	Short: "Search the memory.db archive",
	Long: `Searches memory.db, the archive of what ox missions learned. Since 1.0
threads keep memories in Claude Code's auto-memory store, which
` + "`ox memory audit`" + ` checks.

Examples:
  ox learnings                    # List all
  ox learnings --category gotcha  # Filter by category
  ox learnings --tag backend      # Filter by tag
  ox learnings --limit 10         # Limit results`,
	RunE: runLearnings,
}

var (
	learningsCategory string
	learningsTag      string
	learningsLimit    int
)

// legacyKind maps the old learning categories onto memory kinds.
var legacyKind = map[string]string{
	"approach": "learning",
	"gotcha":   "gotcha",
	"tool":     "tool",
	"pattern":  "convention",
	"general":  "learning",
	"":         "learning",
}

func runLearnings(cmd *cobra.Command, args []string) error {
	store, err := openMemoryStore()
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	query := learningsTag
	if query == "" {
		query = learningsCategory
	}
	if query == "" {
		counts, err := store.Count()
		if err != nil {
			return err
		}
		if len(counts) == 0 {
			fmt.Println("The memory.db archive is empty.")
			return nil
		}
		fmt.Println("Memory counts by scope (use 'ox memory recall <query>' to search):")
		total := 0
		for scope, n := range counts {
			fmt.Printf("  %-30s %d\n", scope, n)
			total += n
		}
		fmt.Printf("  %-30s %d\n", "TOTAL", total)
		return nil
	}

	var kinds []string
	if k := legacyKind[learningsCategory]; learningsCategory != "" && k != "" {
		kinds = []string{k}
	}
	k := learningsLimit
	if k == 0 {
		k = 10
	}
	mems, degraded, err := store.Search(cmd.Context(), query, memory.SearchOptions{Kinds: kinds, K: k})
	if err != nil {
		return err
	}
	if degraded {
		fmt.Println("(FTS-only — embeddings unavailable)")
	}
	for _, m := range mems {
		fmt.Printf("  [%s] %s (%s)\n      %s\n", m.Kind, m.Title, m.Scope, m.Content)
	}
	if len(mems) == 0 {
		fmt.Println("No matches")
	}
	return nil
}

func init() {
	learningsCmd.Flags().StringVarP(&learningsCategory, "category", "c", "", "Filter by category")
	learningsCmd.Flags().StringVarP(&learningsTag, "tag", "t", "", "Filter by tag")
	learningsCmd.Flags().IntVarP(&learningsLimit, "limit", "n", 0, "Limit results")

	rootCmd.AddCommand(learningsCmd)
}
