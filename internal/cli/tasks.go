package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/yokehelper"
	"github.com/spf13/cobra"
)

var (
	tasksAll    bool
	tasksStatus string
)

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List tasks from yoke",
	Long: `Lists tasks from the yoke task database.

By default shows active tasks (pending, in_progress, blocked).
Use --all to see all tasks including done/dropped.

Examples:
  ox tasks               # List active tasks
  ox tasks --all         # List all tasks
  ox tasks --status pending  # List only pending tasks`,
	RunE: runTasks,
}

func runTasks(cmd *cobra.Command, args []string) error {
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return fmt.Errorf("connect to yoke: %w", err)
	}
	defer yokeClient.Close()

	tasks, err := yokeClient.List(tasksAll, tasksStatus)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found")
		return nil
	}

	// Print header
	fmt.Printf("%-5s %-12s %-4s %-50s %s\n", "SEQ", "STATUS", "PRI", "TITLE", "TAGS")
	fmt.Println(strings.Repeat("-", 90))

	for _, t := range tasks {
		title := t.Title
		if len(title) > 48 {
			title = title[:45] + "..."
		}

		tags := ""
		if len(t.Tags) > 0 {
			tags = strings.Join(t.Tags, ", ")
		}

		fmt.Printf("%-5d %-12s P%-3d %-50s %s\n",
			t.Seq,
			t.Status,
			t.Priority,
			title,
			tags,
		)
	}

	return nil
}

func init() {
	tasksCmd.Flags().BoolVarP(&tasksAll, "all", "a", false, "Show all tasks including done/dropped")
	tasksCmd.Flags().StringVarP(&tasksStatus, "status", "s", "", "Filter by status")

	rootCmd.AddCommand(tasksCmd)
}
