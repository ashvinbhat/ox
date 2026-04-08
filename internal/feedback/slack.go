package feedback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// SlackMessage represents a Slack webhook message
type SlackMessage struct {
	Text        string       `json:"text,omitempty"`
	Blocks      []SlackBlock `json:"blocks,omitempty"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

// SlackBlock represents a Slack block element
type SlackBlock struct {
	Type     string      `json:"type"`
	Text     *SlackText  `json:"text,omitempty"`
	Elements []SlackElement `json:"elements,omitempty"`
}

// SlackText represents text in a Slack block
type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SlackElement represents an element in a Slack block
type SlackElement struct {
	Type string     `json:"type"`
	Text *SlackText `json:"text,omitempty"`
	URL  string     `json:"url,omitempty"`
}

// SlackAttachment represents a Slack attachment
type SlackAttachment struct {
	Color string `json:"color,omitempty"`
	Text  string `json:"text,omitempty"`
}

// SendWeeklyReminder sends a weekly feedback reminder to Slack
func SendWeeklyReminder(webhookURL string, dashboardURL string, tasksThisWeek []TaskInfo, pendingWeeks int) error {
	// Build task list
	taskList := ""
	if len(tasksThisWeek) > 0 {
		for _, t := range tasksThisWeek {
			taskList += fmt.Sprintf("• #%d %s\n", t.Seq, t.Title)
		}
	} else {
		taskList = "• No tasks tracked this week\n"
	}

	// Build pending weeks message
	pendingMsg := ""
	if pendingWeeks > 0 {
		pendingMsg = fmt.Sprintf("⚠️ *%d week(s) pending review*", pendingWeeks)
	} else {
		pendingMsg = "✅ All weeks up to date"
	}

	msg := SlackMessage{
		Blocks: []SlackBlock{
			{
				Type: "header",
				Text: &SlackText{
					Type: "plain_text",
					Text: "🐂 Weekly Feedback Reminder",
				},
			},
			{
				Type: "section",
				Text: &SlackText{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*Tasks this week:*\n%s", taskList),
				},
			},
			{
				Type: "section",
				Text: &SlackText{
					Type: "mrkdwn",
					Text: pendingMsg,
				},
			},
			{
				Type: "section",
				Text: &SlackText{
					Type: "mrkdwn",
					Text: "Log your collaborator observations for the feedback cycle.",
				},
			},
			{
				Type: "actions",
				Elements: []SlackElement{
					{
						Type: "button",
						Text: &SlackText{Type: "plain_text", Text: "Log Feedback"},
						URL:  dashboardURL + "/feedback/week",
					},
					{
						Type: "button",
						Text: &SlackText{Type: "plain_text", Text: "View All"},
						URL:  dashboardURL + "/feedback",
					},
				},
			},
		},
	}

	return sendSlackMessage(webhookURL, msg)
}

// SendCycleReminder sends a reminder that feedback cycle is approaching
func SendCycleReminder(webhookURL string, dashboardURL string, cycle string, dueDate string, collaboratorCount int) error {
	msg := SlackMessage{
		Blocks: []SlackBlock{
			{
				Type: "header",
				Text: &SlackText{
					Type: "plain_text",
					Text: "🔔 Feedback Cycle Approaching",
				},
			},
			{
				Type: "section",
				Text: &SlackText{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*%s Cycle*\nFeedback due: *%s*", cycle, dueDate),
				},
			},
			{
				Type: "section",
				Text: &SlackText{
					Type: "mrkdwn",
					Text: fmt.Sprintf("You have observations for *%d collaborators*. Time to prepare your SBI feedback!", collaboratorCount),
				},
			},
			{
				Type: "actions",
				Elements: []SlackElement{
					{
						Type: "button",
						Text: &SlackText{Type: "plain_text", Text: "Prepare Feedback"},
						URL:  dashboardURL + "/feedback/prepare",
					},
				},
			},
		},
	}

	return sendSlackMessage(webhookURL, msg)
}

func sendSlackMessage(webhookURL string, msg SlackMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal slack message: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("send slack message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	return nil
}
