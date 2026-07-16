package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "What needs you, across all missions",
	Long: `One ranked list of every decision waiting on you: unanswered worker
blockers, plans awaiting review, ship approvals, parked PRs. Empty output
means every mission is either working or waiting on someone else.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		missions, err := mission.List(cfg.Home)
		if err != nil {
			return err
		}

		type item struct {
			m      *mission.Mission
			reason string
			rank   int
			inFor  time.Duration
		}
		var items []item
		for _, m := range missions {
			reason := harness.PendingOnUser(m)
			if reason == "" {
				continue
			}
			rank := 4
			switch {
			case strings.HasPrefix(reason, "blocked:"):
				rank = 1
			case strings.HasPrefix(reason, "plan awaiting"):
				rank = 2
			case reason == "ship approval pending":
				rank = 3
			}
			inFor := time.Duration(0)
			if n := len(m.PhaseHistory); n > 0 {
				inFor = time.Since(m.PhaseHistory[n-1].At)
			}
			items = append(items, item{m, reason, rank, inFor})
		}

		if len(items) == 0 {
			fmt.Println("Nothing pending on you — all missions working or waiting on others.")
			return nil
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].rank != items[j].rank {
				return items[i].rank < items[j].rank
			}
			return items[i].inFor > items[j].inFor
		})

		fmt.Printf("%d item(s) pending on you:\n\n", len(items))
		for _, it := range items {
			task := ""
			if it.m.Yoke != nil {
				task = fmt.Sprintf(" (#%d)", it.m.Yoke.Seq)
			}
			fmt.Printf("  %-6s %-10s %-46s %s\n", it.m.ID, it.m.Phase+" "+humanAge(time.Now().Add(-it.inFor)), it.reason, firstN(it.m.Goal, 38)+task)
		}
		return nil
	},
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func init() {
	rootCmd.AddCommand(inboxCmd)
}
