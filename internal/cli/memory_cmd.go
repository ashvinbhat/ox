package cli

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/ashvinbhat/ox/internal/memory/embed"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage the long-term memory store",
}

var memoryMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Import legacy learnings.db into memory.db (idempotent)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		store, err := openMemoryStore()
		if err != nil {
			return err
		}
		defer store.Close()

		repoNames := map[string]bool{}
		for name := range cfg.Repos {
			repoNames[name] = true
		}
		migrated, skipped, err := store.MigrateLearnings(context.Background(), cfg.Home, repoNames)
		if err != nil {
			return err
		}
		fmt.Printf("Migrated %d learnings (%d already present)\n", migrated, skipped)
		if migrated > 0 {
			fmt.Println("Run 'ox memory backfill' to embed them for semantic recall.")
		}
		return nil
	},
}

var backfillReembed bool

var memoryBackfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Embed memories that lack vectors for the current provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openMemoryStore()
		if err != nil {
			return err
		}
		defer store.Close()

		n, err := store.Backfill(context.Background(), backfillReembed)
		if err != nil {
			return fmt.Errorf("backfill (embedded %d first): %w", n, err)
		}
		fmt.Printf("Embedded %d memories\n", n)
		return nil
	},
}

var memoryGCCmd = &cobra.Command{
	Use:   "gc",
	Short: "Archive superseded and long-unused memories",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openMemoryStore()
		if err != nil {
			return err
		}
		defer store.Close()

		n, err := store.GC(nil)
		if err != nil {
			return err
		}
		fmt.Printf("Archived %d memories\n", n)
		return nil
	},
}

var memoryStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show active memory counts per scope",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		store, err := openMemoryStore()
		if err != nil {
			return err
		}
		defer store.Close()

		counts, err := store.Count()
		if err != nil {
			return err
		}
		if len(counts) == 0 {
			fmt.Println("No memories yet")
			return nil
		}

		scopes := make([]string, 0, len(counts))
		total := 0
		for s, n := range counts {
			scopes = append(scopes, s)
			total += n
		}
		sort.Strings(scopes)
		for _, s := range scopes {
			fmt.Printf("  %-30s %d\n", s, counts[s])
		}
		fmt.Printf("  %-30s %d\n", "TOTAL", total)

		if e := embed.New(cfg.Memory.Embeddings); e == nil {
			fmt.Println("\nEmbeddings: not configured (FTS-only recall). Set memory.embeddings in ox.yaml.")
		} else {
			fmt.Printf("\nEmbeddings: %s\n", e.Model())
		}
		return nil
	},
}

var memoryRecallQuery string

var memoryRecallCmd = &cobra.Command{
	Use:   "recall <query>",
	Short: "Search memories from the command line",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openMemoryStore()
		if err != nil {
			return err
		}
		defer store.Close()

		query := args[0]
		mems, degraded, err := store.Search(context.Background(), query, memory.SearchOptions{K: 8})
		if err != nil {
			return err
		}
		if degraded {
			fmt.Println("(FTS-only — embeddings unavailable)")
		}
		if len(mems) == 0 {
			fmt.Println("No matches")
			return nil
		}
		for _, m := range mems {
			fmt.Printf("[%s] %s (%s, score %.3f)\n    %s\n", m.Kind, m.Title, m.Scope, m.Score, m.Content)
		}
		return nil
	},
}

func openMemoryStore() (*memory.Store, error) {
	cfg := requireConfig()
	return memory.Open(cfg.Home, embed.New(cfg.Memory.Embeddings))
}

func init() {
	memoryBackfillCmd.Flags().BoolVar(&backfillReembed, "re-embed", false, "Re-embed everything (after provider/model change)")
	memoryCmd.AddCommand(memoryMigrateCmd, memoryBackfillCmd, memoryGCCmd, memoryStatsCmd, memoryRecallCmd)
	rootCmd.AddCommand(memoryCmd)
}
