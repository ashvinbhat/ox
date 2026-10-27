package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/journal"
	"github.com/ashvinbhat/ox/internal/tracks"
)

var (
	journalDays  int
	journalTrack string
	journalJSON  bool

	entryTrack, entryThread string
	decided, over, why      string
	grade, predicted        string
	actual, gap             string
	recapSolid, recapShaky  string
	recapPeriod             string
)

var journalCmd = &cobra.Command{
	Use:   "journal",
	Short: "Read the decision journal: today, or the last N days",
	Long: `The decision journal is one markdown file a day in ~/.ox/journal. Threads log
each real choice as they make it (ox journal add), own-it rounds
(ox journal own-it) and weekly recaps (ox journal recap). Each entry carries
the time and the thread's track and title, taken from the desktop thread the
command runs in.

  ox journal              today
  ox journal --days 7     the last week
  ox journal --track ox   one track
  ox journal status       counts, own-it rounds left today, whether a recap is due`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runJournal,
}

var journalAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Log a decision: what was chosen, over what, and why",
	Example: `  ox journal add --decided "journal as markdown day files" \
    --over "JSONL (needs a renderer to read)" --why "Ashvin reads it directly"`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if decided == "" || over == "" || why == "" {
			return errors.New("--decided, --over and --why are all needed: the choice, what it beat, and why")
		}
		return logEntry(journal.Decision, []journal.Field{{Key: "Decided", Value: decided}, {Key: "Over", Value: over}, {Key: "Why", Value: why}})
	},
}

var journalOwnItCmd = &cobra.Command{
	Use:          "own-it",
	Short:        "Log an own-it round: Ashvin's predicted approach against the one built",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch grade {
		case journal.GradeMatch, journal.GradePartial, journal.GradeMiss:
		default:
			return fmt.Errorf("--grade is %s, %s or %s", journal.GradeMatch, journal.GradePartial, journal.GradeMiss)
		}
		if predicted == "" || actual == "" {
			return errors.New("--predicted and --actual are needed")
		}
		return logEntry(journal.OwnIt, []journal.Field{{Key: "Own-it", Value: grade}, {Key: "Predicted", Value: predicted}, {Key: "Actual", Value: actual}, {Key: "Gap", Value: gap}})
	},
}

var journalRecapCmd = &cobra.Command{
	Use:          "recap",
	Short:        "Log a weekly recap: which concepts held up and which were shaky",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if recapSolid == "" && recapShaky == "" {
			return errors.New("--solid or --shaky is needed")
		}
		period := recapPeriod
		if period == "" {
			period = "week to " + time.Now().Format("Mon 2 Jan")
		}
		return logEntry(journal.Recap, []journal.Field{{Key: "Recap", Value: period}, {Key: "Solid", Value: recapSolid}, {Key: "Shaky", Value: recapShaky}})
	},
}

var journalStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Counts, own-it rounds left today, and whether a recap is due",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runJournalStatus,
}

func logEntry(kind journal.Kind, fields []journal.Field) error {
	cfg := requireConfig()
	thread, track, _ := journalContext(cfg.Home)
	path, err := journal.Append(cfg.Home, journal.Entry{At: time.Now(), Kind: kind, Track: track, Thread: thread, Fields: fields})
	if err != nil {
		return err
	}
	fmt.Printf("Logged to %s\n", tildePath(path))
	return nil
}

// journalContext is the thread an entry comes from: the desktop thread this
// command runs in, unless --thread or --track name it.
func journalContext(oxHome string) (thread, track string, own bool) {
	ts, _ := tracks.Load(oxHome)
	if id := os.Getenv("CLAUDE_CODE_HOST_SESSION_ID"); id != "" {
		if s, err := desktop.Get(desktop.DefaultDir(), id); err == nil {
			thread = s.Title
			if t := sessionTrack(s, ts); t != nil {
				track, own = t.Label(), t.Own
			}
		}
	}
	if entryThread != "" {
		thread = entryThread
	}
	if entryTrack != "" {
		track, own = entryTrack, false
		for _, t := range ts {
			if t.Label() == entryTrack {
				own = t.Own
			}
		}
	}
	return thread, track, own
}

func runJournal(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	now := time.Now()
	days := max(journalDays, 1)
	entries, err := journal.Read(cfg.Home, now.AddDate(0, 0, -(days-1)), now)
	if err != nil {
		return err
	}
	if journalTrack != "" {
		kept := entries[:0]
		for _, e := range entries {
			if e.Track == journalTrack {
				kept = append(kept, e)
			}
		}
		entries = kept
	}

	if journalJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}
	if len(entries) == 0 {
		if days == 1 {
			fmt.Println("Nothing in the journal today. `ox journal --days 7` for the week.")
		} else {
			fmt.Printf("Nothing in the journal in the last %d days.\n", days)
		}
		return nil
	}
	day := ""
	for _, e := range entries {
		if d := e.At.Format("Mon 2 Jan"); d != day && days > 1 {
			day = d
			fmt.Printf("## %s\n\n", d)
		}
		fmt.Println(journal.Format(e))
	}
	return nil
}

type journalStatusView struct {
	journal.Status
	Thread    string `json:"thread,omitempty"`
	Track     string `json:"track,omitempty"`
	Own       bool   `json:"ownTrack"`
	OwnItLeft int    `json:"ownItLeft"`
}

func runJournalStatus(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	now := time.Now()
	st, err := journalStatus(cfg.Home, now)
	if err != nil {
		return err
	}
	thread, track, own := journalContext(cfg.Home)
	v := journalStatusView{Status: st, Thread: thread, Track: track, Own: own, OwnItLeft: max(journal.OwnItPerDay-st.OwnItToday, 0)}

	if journalJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	fmt.Printf("Today: %s · own-it %d of %d\n", countOf(st.DecisionsToday, "decision"), st.OwnItToday, journal.OwnItPerDay)
	line := "Last 7 days: " + countOf(st.DecisionsWeek, "decision")
	if g := gradesLine(st.GradesWeek); g != "" {
		line += " · own-it " + g
	}
	fmt.Println(line)
	switch {
	case st.RecapDue:
		fmt.Println("Recap: due (/recap)")
	case !st.LastRecap.IsZero():
		fmt.Printf("Recap: last on %s\n", st.LastRecap.Format("Mon 2 Jan"))
	default:
		fmt.Println("Recap: none in the last two weeks")
	}
	if thread != "" || track != "" {
		label := orNone(track)
		if own {
			label += " (own track"
			if v.OwnItLeft > 0 {
				label += fmt.Sprintf(": offer own-it, %d left today)", v.OwnItLeft)
			} else {
				label += ": no own-it rounds left today)"
			}
		}
		fmt.Printf("This thread: %s · %s\n", clip(orNone(thread), 50), label)
	}
	return nil
}

// journalStatus reads two weeks back so the last recap is found even when
// none happened this week.
func journalStatus(oxHome string, now time.Time) (journal.Status, error) {
	entries, err := journal.Read(oxHome, now.AddDate(0, 0, -13), now)
	if err != nil {
		return journal.Status{}, err
	}
	return journal.Summarize(entries, now), nil
}

// gradesLine is "2 match, 1 partial" in match, partial, miss order.
func gradesLine(grades map[string]int) string {
	order := map[string]int{journal.GradeMatch: 0, journal.GradePartial: 1, journal.GradeMiss: 2}
	var keys []string
	for k, n := range grades {
		if n > 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return order[keys[i]] < order[keys[j]] })
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", grades[k], k))
	}
	return strings.Join(parts, ", ")
}

func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func init() {
	journalCmd.Flags().IntVar(&journalDays, "days", 1, "How many days back, today included")
	journalCmd.Flags().StringVar(&journalTrack, "track", "", "Only entries on this track (short name)")
	journalCmd.Flags().BoolVar(&journalJSON, "json", false, "Machine-readable output")
	journalStatusCmd.Flags().BoolVar(&journalJSON, "json", false, "Machine-readable output")

	for _, c := range []*cobra.Command{journalAddCmd, journalOwnItCmd, journalRecapCmd} {
		c.Flags().StringVar(&entryTrack, "track", "", "Track short name (default: the thread's track)")
		c.Flags().StringVar(&entryThread, "thread", "", "Thread title (default: the desktop thread this runs in)")
	}
	journalAddCmd.Flags().StringVar(&decided, "decided", "", "What was chosen")
	journalAddCmd.Flags().StringVar(&over, "over", "", "The options it beat")
	journalAddCmd.Flags().StringVar(&why, "why", "", "Why it won")
	journalOwnItCmd.Flags().StringVar(&grade, "grade", "", "match, partial or miss")
	journalOwnItCmd.Flags().StringVar(&predicted, "predicted", "", "Ashvin's predicted approach")
	journalOwnItCmd.Flags().StringVar(&actual, "actual", "", "The approach built")
	journalOwnItCmd.Flags().StringVar(&gap, "gap", "", "What differed, and why")
	journalRecapCmd.Flags().StringVar(&recapSolid, "solid", "", "Concepts he explained well")
	journalRecapCmd.Flags().StringVar(&recapShaky, "shaky", "", "Concepts to revisit")
	journalRecapCmd.Flags().StringVar(&recapPeriod, "period", "", `What it covers (default "week to <today>")`)

	journalCmd.AddCommand(journalAddCmd, journalOwnItCmd, journalRecapCmd, journalStatusCmd)
	rootCmd.AddCommand(journalCmd)
}
