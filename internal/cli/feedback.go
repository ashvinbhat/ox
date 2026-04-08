package cli

import (
	"fmt"
	"os"

	"github.com/ashvinbhat/ox/internal/feedback"
	"github.com/spf13/cobra"
)

var feedbackCmd = &cobra.Command{
	Use:   "feedback",
	Short: "Feedback tracking commands",
	Long:  `Commands for managing feedback observations and reminders.`,
}

var feedbackReminderCmd = &cobra.Command{
	Use:   "reminder",
	Short: "Send weekly feedback reminder to Slack",
	Long: `Sends a weekly reminder to the configured Slack webhook.

Requires SLACK_WEBHOOK_URL environment variable to be set.`,
	RunE: runFeedbackReminder,
}

var feedbackStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show feedback status for current cycle",
	RunE:  runFeedbackStatus,
}

func runFeedbackReminder(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Check config first, then environment variable
	webhookURL := cfg.SlackWebhookURL
	if webhookURL == "" {
		webhookURL = os.Getenv("SLACK_WEBHOOK_URL")
	}
	if webhookURL == "" {
		return fmt.Errorf("Slack webhook not configured. Set slack_webhook_url in ox.yaml or SLACK_WEBHOOK_URL env var")
	}

	store := feedback.NewStore(cfg.Home)
	if err := store.Init(); err != nil {
		return fmt.Errorf("init feedback store: %w", err)
	}

	// Get pending weeks
	pending, err := store.GetPendingWeeks()
	if err != nil {
		return fmt.Errorf("get pending weeks: %w", err)
	}

	// Get tasks this week (from yoke)
	// For now, use empty list - can be enhanced later
	var tasks []feedback.TaskInfo

	// Get dashboard URL
	port := cfg.DashboardPort
	if port == 0 {
		port = 8080
	}
	dashboardURL := fmt.Sprintf("http://localhost:%d", port)

	if err := feedback.SendWeeklyReminder(webhookURL, dashboardURL, tasks, len(pending)); err != nil {
		return fmt.Errorf("send reminder: %w", err)
	}

	fmt.Println("Weekly reminder sent to Slack")
	return nil
}

func runFeedbackStatus(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	store := feedback.NewStore(cfg.Home)
	if err := store.Init(); err != nil {
		return fmt.Errorf("init feedback store: %w", err)
	}

	// Get current week
	currentWeek := store.GetCurrentWeek()
	fmt.Printf("Current Week: %s\n\n", currentWeek)

	// Get pending weeks
	pending, err := store.GetPendingWeeks()
	if err != nil {
		return fmt.Errorf("get pending weeks: %w", err)
	}

	if len(pending) > 0 {
		fmt.Printf("Pending Weeks (%d):\n", len(pending))
		for _, w := range pending {
			fmt.Printf("  - %s\n", w)
		}
	} else {
		fmt.Println("All weeks up to date!")
	}
	fmt.Println()

	// Get summaries
	summaries, err := store.GetPersonSummaries()
	if err != nil {
		return fmt.Errorf("get summaries: %w", err)
	}

	if len(summaries) > 0 {
		fmt.Printf("People This Cycle (%d):\n", len(summaries))
		for _, s := range summaries {
			fmt.Printf("  %s: %d observations (%d strengths, %d growth)\n",
				s.Person.Name, s.TotalCount, len(s.Strengths), len(s.GrowthAreas))
		}
	} else {
		fmt.Println("No observations this cycle yet.")
	}

	return nil
}

func init() {
	feedbackCmd.AddCommand(feedbackReminderCmd)
	feedbackCmd.AddCommand(feedbackStatusCmd)
	rootCmd.AddCommand(feedbackCmd)
}
