package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokecli"
	"github.com/spf13/cobra"
)

var statusAll bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active task workspaces",
	Long: `Shows information about active task workspaces.

By default shows all workspaces. Use task ID to show specific workspace.`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	workspaces, err := workspace.List(cfg.Home)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Println("No active workspaces")
		fmt.Println("\nRun 'ox pickup <task-id> --repos <name>' to create one")
		return nil
	}

	fmt.Printf("%-6s %-40s %-15s %s\n", "TASK", "TITLE", "REPOS", "PATH")
	fmt.Println(strings.Repeat("-", 80))

	for _, ws := range workspaces {
		title := "(unknown)"
		var taskID string
		if t, err := yokecli.Get(fmt.Sprintf("%d", ws.TaskSeq)); err == nil {
			title = t.Title
			taskID = t.ID
			if len(title) > 35 {
				title = title[:32] + "..."
			}
		}

		repos := strings.Join(ws.Repos, ",")
		if repos == "" {
			repos = "-"
		}

		fmt.Printf("#%-5d %-40s %-15s %s\n", ws.TaskSeq, title, repos, ws.Path)

		// Surface linked PRs (from yoke notes) so the user sees what's shipped.
		if taskID != "" {
			if notes, err := yokecli.Notes(taskID); err == nil {
				for _, n := range notes {
					if strings.HasPrefix(n.Content, "PR ") || strings.HasPrefix(n.Content, "PR(") {
						fmt.Printf("       %s\n", n.Content)
					}
				}
			}
		}
	}

	return nil
}

func init() {
	statusCmd.Flags().BoolVarP(&statusAll, "all", "a", false, "Show all details")
	rootCmd.AddCommand(statusCmd)
}
