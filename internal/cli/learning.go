package cli

import (
	"fmt"

	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/spf13/cobra"
)

var (
	learnCategory string
	learnTags     []string
)

var learnCmd = &cobra.Command{
	Use:   "learn <insight>",
	Short: "Capture a learning or insight",
	Long: `Captures a learning that can be surfaced in future similar tasks.

Learnings are tagged and categorized for easy retrieval.
When in a workspace, the current task's tags are auto-added.

Categories:
  approach  - Approaches that worked
  gotcha    - Gotchas and pitfalls discovered
  tool      - Tool preferences and tips
  pattern   - Patterns observed
  general   - General insights (default)

Examples:
  ox learn "MongoDB aggregations need index hints for performance"
  ox learn "Use feature flags for gradual rollout" --category approach
  ox learn "REST Assured better than Karate for API tests" -c tool -t api,testing`,
	Args: cobra.ExactArgs(1),
	RunE: runLearn,
}

var learningsCmd = &cobra.Command{
	Use:   "learnings",
	Short: "List captured learnings",
	Long: `Lists learnings, optionally filtered by category or tag.

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

func runLearn(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	content := args[0]

	store, err := openMemoryStore()
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close()

	kind := legacyKind[learnCategory]
	if kind == "" {
		kind = learnCategory // allow memory-native kinds directly
	}

	tags := learnTags
	scope := "global"
	if ws, err := getCurrentWorkspace(cfg.Home); err == nil {
		for _, repo := range ws.Repos {
			tags = append(tags, repo)
			if scope == "global" {
				scope = "repo:" + repo
			}
		}
	}

	res, err := store.Remember(cmd.Context(), memory.RememberInput{
		Content: content, Kind: kind, Scope: scope, Tags: tags, Source: "user",
	})
	if err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	switch {
	case res.Status == "created":
		fmt.Printf("Remembered (%s, %s, %s)\n", res.UID, kind, scope)
	default:
		fmt.Printf("Memory %s (%s)\n", res.Status, res.UID)
	}
	return nil
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
			fmt.Println("No memories yet. Use 'ox learn \"your insight\"' to capture one.")
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
	learnCmd.Flags().StringVarP(&learnCategory, "category", "c", "", "Category (approach, gotcha, tool, pattern, general)")
	learnCmd.Flags().StringSliceVarP(&learnTags, "tag", "t", nil, "Tags for this learning (repeatable)")

	learningsCmd.Flags().StringVarP(&learningsCategory, "category", "c", "", "Filter by category")
	learningsCmd.Flags().StringVarP(&learningsTag, "tag", "t", "", "Filter by tag")
	learningsCmd.Flags().IntVarP(&learningsLimit, "limit", "n", 0, "Limit results")

	rootCmd.AddCommand(learnCmd)
	rootCmd.AddCommand(learningsCmd)
}
