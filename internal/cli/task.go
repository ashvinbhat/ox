package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/yoke"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task <id>",
	Short: "Show task details",
	Long: `Shows detailed information about a task.

Examples:
  ox task 9          # Show task #9
  ox task abc123     # Show task by ID`,
	Args: cobra.ExactArgs(1),
	RunE: runTask,
}

func runTask(cmd *cobra.Command, args []string) error {
	yokeClient, err := yoke.NewClient()
	if err != nil {
		return fmt.Errorf("connect to yoke: %w", err)
	}
	defer yokeClient.Close()

	t, err := yokeClient.Get(args[0])
	if err != nil {
		return err
	}

	// Print task details
	fmt.Printf("Task #%d: %s\n", t.Seq, t.Title)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Status:   %s\n", t.Status)
	fmt.Printf("Priority: P%d\n", t.Priority)

	if len(t.Tags) > 0 {
		fmt.Printf("Tags:     %s\n", strings.Join(t.Tags, ", "))
	}

	if t.NotionURL != nil {
		fmt.Printf("Notion:   %s\n", *t.NotionURL)
	}

	if t.Body != "" {
		fmt.Println()
		fmt.Println("Description:")
		fmt.Println(t.Body)
	}

	// Show notes
	notes, err := yokeClient.GetNotes(t.ID)
	if err == nil && len(notes) > 0 {
		fmt.Println()
		fmt.Println("Notes:")
		for _, n := range notes {
			fmt.Printf("  [%s] %s\n", n.CreatedAt.Format("2006-01-02"), n.Content)
		}
	}

	// Show blockers
	if len(t.Blockers) > 0 {
		fmt.Println()
		fmt.Println("Blockers:")
		for _, b := range t.Blockers {
			fmt.Printf("  - %s\n", b)
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(taskCmd)
}
