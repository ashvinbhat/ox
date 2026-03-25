package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/learning"
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

func runLearn(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	content := args[0]

	store, err := learning.NewStore(cfg.Home)
	if err != nil {
		return fmt.Errorf("open learning store: %w", err)
	}
	defer store.Close()

	// Determine category
	category := learning.Category(learnCategory)
	if category == "" {
		category = learning.CategoryGeneral
	}

	// Collect tags
	tags := learnTags

	// If in a workspace, auto-add task tags and get task seq
	var taskSeq *int
	if ws, err := getCurrentWorkspace(cfg.Home); err == nil {
		taskSeq = &ws.TaskSeq

		// Add repo names as tags
		for _, repo := range ws.Repos {
			tags = append(tags, repo)
		}
	}

	l, err := store.Add(content, category, tags, taskSeq)
	if err != nil {
		return fmt.Errorf("save learning: %w", err)
	}

	fmt.Printf("Learning captured (#%d)\n", l.ID)
	fmt.Printf("  Category: %s\n", l.Category)
	if len(l.Tags) > 0 {
		fmt.Printf("  Tags: %s\n", strings.Join(l.Tags, ", "))
	}
	if l.TaskSeq != nil {
		fmt.Printf("  Task: #%d\n", *l.TaskSeq)
	}

	return nil
}

func runLearnings(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	store, err := learning.NewStore(cfg.Home)
	if err != nil {
		return fmt.Errorf("open learning store: %w", err)
	}
	defer store.Close()

	opts := learning.ListOptions{
		Category: learning.Category(learningsCategory),
		Tag:      learningsTag,
		Limit:    learningsLimit,
	}

	learnings, err := store.List(opts)
	if err != nil {
		return fmt.Errorf("list learnings: %w", err)
	}

	if len(learnings) == 0 {
		fmt.Println("No learnings captured yet.")
		fmt.Println("Use 'ox learn \"your insight\"' to capture one.")
		return nil
	}

	count, _ := store.Count()
	if opts.Category != "" || opts.Tag != "" {
		fmt.Printf("Learnings (showing %d of %d):\n\n", len(learnings), count)
	} else {
		fmt.Printf("Learnings (%d total):\n\n", count)
	}

	for _, l := range learnings {
		// Truncate content for display
		content := l.Content
		if len(content) > 70 {
			content = content[:67] + "..."
		}

		fmt.Printf("  #%d [%s] %s\n", l.ID, l.Category, content)
		if len(l.Tags) > 0 {
			fmt.Printf("      Tags: %s\n", strings.Join(l.Tags, ", "))
		}
		if l.TaskSeq != nil {
			fmt.Printf("      From task #%d\n", *l.TaskSeq)
		}
		fmt.Println()
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
