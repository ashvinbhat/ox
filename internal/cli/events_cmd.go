package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/watcher"
)

var eventsCmd = &cobra.Command{
	Use:    "events",
	Hidden: true,
	Short:  "Mission event plumbing (used by session hooks)",
}

var eventsAttachMission string

// events attach is the orchestrator's UserPromptSubmit hook: it delivers all
// unread mission events as invisible context on the user's own message, so
// nothing ever has to be typed into the conversation to keep the orc current.
var eventsAttachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Emit unread mission events as UserPromptSubmit hook context",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		m, err := mission.Open(cfg.Home, eventsAttachMission)
		if err != nil {
			return nil // never break the user's prompt over plumbing
		}

		var lines []string
		mission.WithLock(cfg.Home, m.ID, func() error {
			cursor := readHookCursor(m)
			events, err := m.EventsSince(cursor)
			if err != nil || len(events) == 0 {
				return nil
			}
			maxN := cursor
			for _, ev := range events {
				if ev.N > maxN {
					maxN = ev.N
				}
				if line := watcher.EventLine(ev); line != "" {
					lines = append(lines, fmt.Sprintf("[%s] %s", ev.TS.Format("15:04"), line))
				}
			}
			writeHookCursor(m, maxN)
			return nil
		})

		if len(lines) == 0 {
			return nil
		}

		ctx := "Mission events since your last turn (system context — act on what needs action, " +
			"mention to the user only what matters, never echo these lines):\n" + strings.Join(lines, "\n")
		out, _ := json.Marshal(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "UserPromptSubmit",
				"additionalContext": ctx,
			},
		})
		fmt.Println(string(out))
		return nil
	},
}

func hookCursorPath(m *mission.Mission) string {
	return filepath.Join(m.Dir(), "hook-cursor")
}

func readHookCursor(m *mission.Mission) int64 {
	data, err := os.ReadFile(hookCursorPath(m))
	if err != nil {
		return 0
	}
	var n int64
	fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &n)
	return n
}

func writeHookCursor(m *mission.Mission, n int64) {
	os.WriteFile(hookCursorPath(m), []byte(fmt.Sprintf("%d", n)), 0o644)
}

func init() {
	eventsAttachCmd.Flags().StringVar(&eventsAttachMission, "mission", "", "Mission ID")
	eventsAttachCmd.MarkFlagRequired("mission")
	eventsCmd.AddCommand(eventsAttachCmd)
	rootCmd.AddCommand(eventsCmd)
}
