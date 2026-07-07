package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ashvinbhat/ox/internal/harness"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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

var memoryBootstrapCmd = &cobra.Command{
	Use:   "bootstrap <repo>",
	Short: "Generate a repo's initial knowledge doc with a one-shot explorer",
	Long: `Runs a headless explorer over the repo's base clone and saves the result as
~/.ox/memory/repos/<repo>.md — the living doc workers get in their briefs and
the distiller keeps current. Without a bootstrap the doc builds up slowly
across missions; this jumpstarts it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		repo := args[0]
		if _, ok := cfg.Repos[repo]; !ok {
			return fmt.Errorf("repo %q not registered", repo)
		}
		repoPath := filepath.Join(cfg.Home, "repos", repo)

		prompt := fmt.Sprintf(`Explore this repository (%s) and write its working-knowledge document —
the orientation a senior engineer would give a new teammate in 10 minutes.

Structure (exactly these sections, hard caps):
# %s — working knowledge
## Architecture        (what the system is, its modules and how they relate; <= 80 lines)
## Conventions         (how code is actually written here — patterns you verified in multiple places; <= 80 lines)
## Build, test, run    (exact commands that work, verified from build files; <= 40 lines)
## Gotchas             (non-obvious footguns you can evidence from the code; <= 60 lines)
## Key files           (path -> one-line purpose, the 15-25 files that matter most; <= 40 lines)

Rules: <= 300 lines total. Every claim grounded in code you actually read — no guesses.
Prefer specific ("handlers return apperr.E") over generic ("has error handling").
Your final reply must be ONLY the document itself — no preamble, no code fences around it.`, repo, repo)

		fmt.Printf("Exploring %s (this takes a few minutes)...\n", repo)
		c := exec.Command("claude", "-p", "--dangerously-skip-permissions",
			"--output-format", "json", "--model", "sonnet", "--max-turns", "50", "--strict-mcp-config")
		c.Dir = repoPath
		c.Stdin = strings.NewReader(prompt)
		out, err := c.Output()
		if err != nil {
			return fmt.Errorf("explorer failed: %w", err)
		}

		var res struct {
			IsError      bool    `json:"is_error"`
			Result       string  `json:"result"`
			TotalCostUSD float64 `json:"total_cost_usd"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return fmt.Errorf("parse result: %w", err)
		}
		if res.IsError {
			return fmt.Errorf("explorer error: %.300s", res.Result)
		}

		doc := strings.TrimSpace(res.Result)
		doc = strings.TrimPrefix(doc, "```markdown")
		doc = strings.TrimPrefix(doc, "```")
		doc = strings.TrimSpace(strings.TrimSuffix(doc, "```"))

		if err := harness.WriteRepoDoc(cfg.Home, repo, doc, "bootstrap"); err != nil {
			return err
		}
		fmt.Printf("Saved ~/.ox/memory/repos/%s.md (%d lines, $%.2f)\n",
			repo, strings.Count(doc, "\n")+1, res.TotalCostUSD)
		return nil
	},
}

func openMemoryStore() (*memory.Store, error) {
	cfg := requireConfig()
	return memory.Open(cfg.Home, embed.New(cfg.Memory.Embeddings))
}

func init() {
	memoryBackfillCmd.Flags().BoolVar(&backfillReembed, "re-embed", false, "Re-embed everything (after provider/model change)")
	memoryCmd.AddCommand(memoryMigrateCmd, memoryBackfillCmd, memoryGCCmd, memoryStatsCmd, memoryRecallCmd, memoryBootstrapCmd)
	rootCmd.AddCommand(memoryCmd)
}
