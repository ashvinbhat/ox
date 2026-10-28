package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/ghreview"
	"github.com/ashvinbhat/ox/internal/memcheck"
	"github.com/ashvinbhat/ox/internal/tracks"
)

var (
	asksJSON    bool
	asksOffline bool
	asksIfDue   bool
)

// PR state costs a gh call per PR. The ox-brief hook refreshes it hourly in
// the background, so the brief reads a cache and never waits on GitHub.
const prsEvery = time.Hour

var asksCmd = &cobra.Command{
	Use:   "asks",
	Short: "Everything waiting on you, ranked, and the PR fixes threads can make",
	Long: `One queue of what only you can do, in this order: questions from threads,
review asks, your PRs ready to merge, memory suggestions, a due recap. Oldest
first within each. A thread question older than 3 days is marked "still
needed?".

Below it: PR problems a thread can fix on its own (merge conflicts, CI its
change broke, small requested changes). Threads ask you before any big,
design or breaking change.

PR state comes from GitHub, cached in ~/.ox/asks/ and refreshed when older
than an hour (the ox-brief hook does it in the background). The brief shows
the top three.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runAsks,
}

var asksDropCmd = &cobra.Command{
	Use:          "drop <id>...",
	Short:        "Hide asks until they change (ids from --json)",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		dropped, err := asks.LoadDropped(cfg.Home)
		if err != nil {
			return err
		}
		for _, id := range args {
			dropped[id] = time.Now().UTC()
		}
		if err := asks.SaveDropped(cfg.Home, dropped); err != nil {
			return err
		}
		fmt.Printf("Dropped %d. Each comes back if it changes.\n", len(args))
		return nil
	},
}

var asksRefreshCmd = &cobra.Command{
	Use:          "refresh",
	Short:        "Fetch your PRs' review, merge and CI state into the cache",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		if asksIfDue {
			if _, at, err := asks.LoadPRs(cfg.Home); err == nil && time.Since(at) < prsEvery {
				return nil
			}
		}
		_, _, err := refreshPRs(cfg.Home)
		return err
	},
}

func runAsks(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	now := time.Now()
	ts, _ := tracks.Load(cfg.Home)
	sessions, err := desktop.List(desktop.DefaultDir(), false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: read desktop threads: %v\n", err)
	}
	q := collectAsks(cfg.Home, threadsFrom(sessions, ts, now), reviewsToDo(cfg.Home), now, !asksOffline)
	if asksJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(q)
	}
	renderAsks(os.Stdout, q, now)
	return nil
}

// collectAsks builds the queue. With fetch, PR state older than prsEvery is
// fetched from GitHub first; without it the cache is used as it is.
func collectAsks(oxHome string, threads []nowThread, reviews []ghreview.QueueItem, now time.Time, fetch bool) asks.Queue {
	in := asks.Inputs{}
	for _, t := range threads {
		at := asks.Thread{ID: t.SessionID, Title: t.Title, Since: t.LastActive, PRs: t.PRs}
		if t.State == desktop.StateNeedsYou {
			at.Ask = askOrRecap(t)
		}
		in.Threads = append(in.Threads, at)
	}
	for _, r := range reviews {
		in.Reviews = append(in.Reviews, asks.Review{Ref: r.Ref, URL: r.URL, From: asker(r), Why: r.Why,
			ReReview: r.Status == ghreview.QueueReReview, Since: r.Since})
	}
	in.PRs, in.PRsAt, _ = asks.LoadPRs(oxHome)
	if fetch && now.Sub(in.PRsAt) >= prsEvery {
		if prs, at, err := refreshPRs(oxHome); err == nil {
			in.PRs, in.PRsAt = prs, at
		}
	}
	if r, err := memcheck.Pending(oxHome); err == nil {
		in.MemorySuggestions, in.MemoryAt = len(r.Suggestions), r.At
	}
	if st, err := journalStatus(oxHome, now); err == nil {
		in.RecapDue, in.DecisionsWeek = st.RecapDue, st.DecisionsWeek
	}
	dropped, _ := asks.LoadDropped(oxHome)
	return asks.Build(in, dropped, now)
}

// asker is who asked for the review, from the ledger's "asked by <name> (…)",
// or the PR author when nobody's name is on the ask.
func asker(r ghreview.QueueItem) string {
	who, ok := strings.CutPrefix(r.Why, "asked again by ")
	if !ok {
		who, ok = strings.CutPrefix(r.Why, "asked by ")
	}
	if !ok {
		return r.Author
	}
	who, _, _ = strings.Cut(who, " (")
	return who
}

func refreshPRs(oxHome string) ([]asks.PR, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	now := time.Now()
	prs, err := asks.FetchPRs(ctx, now)
	if err != nil {
		return nil, time.Time{}, err
	}
	return prs, now, asks.SavePRs(oxHome, prs, now)
}

func renderAsks(w io.Writer, q asks.Queue, now time.Time) {
	prsAge := "never fetched"
	switch age := now.Sub(q.PRsAt); {
	case q.PRsAt.IsZero():
	case age < time.Minute:
		prsAge = "just fetched"
	default:
		prsAge = "from " + shortAge(age) + " ago"
	}
	if len(q.Waiting) == 0 {
		fmt.Fprintf(w, "Waiting on you: nothing. (PR state %s)\n", prsAge)
	} else {
		fmt.Fprintf(w, "Waiting on you (%d) · PR state %s\n", len(q.Waiting), prsAge)
		for _, a := range q.Waiting {
			line := a.What
			if a.Kind == asks.KindQuestion {
				line = clip(a.Where, 40) + ": " + a.What
			}
			if a.Stale {
				line += " (still needed?)"
			}
			fmt.Fprintf(w, "  %-8s %4s  %-4s %s\n", a.Kind, shortAge(now.Sub(a.Since)), minutes(a.Minutes), clip(line, 100))
			if a.Kind == asks.KindMerge && a.Where != "" {
				fmt.Fprintf(w, "  %-19s → %s\n", "", clip(a.Where, 60))
			}
		}
	}
	if len(q.Fixes) > 0 {
		fmt.Fprintf(w, "\nThreads can fix (%d): conflicts, CI their change broke, small review fixes.\nBig, design or breaking changes go to you first.\n", len(q.Fixes))
		for _, a := range q.Fixes {
			where := "no thread"
			if a.Where != "" {
				where = clip(a.Where, 50)
			}
			fmt.Fprintf(w, "  %-48s → %s\n", clip(a.What, 48), where)
		}
	}
	if q.Dropped > 0 {
		fmt.Fprintf(w, "\n%d dropped.", q.Dropped)
	} else {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "`ox asks drop <id>` hides one until it changes (ids in --json).")
}

// briefAsks is the brief's part: the top three asks, the count of the rest,
// and the PR fixes left to threads.
func briefAsks(q asks.Queue, now time.Time) string {
	var b []byte
	if len(q.Waiting) == 0 {
		b = append(b, "Waiting on you: nothing.\n"...)
	} else {
		b = fmt.Appendf(b, "Waiting on you (%d):\n", len(q.Waiting))
		for i, a := range q.Waiting {
			if i == 3 {
				b = fmt.Appendf(b, "+%d more: `ox asks`\n", len(q.Waiting)-3)
				break
			}
			b = fmt.Appendf(b, "- %s\n", briefAsk(a, now))
		}
	}
	if len(q.Fixes) > 0 {
		var what []string
		for i, a := range q.Fixes {
			if i == 3 {
				what = append(what, fmt.Sprintf("+%d", len(q.Fixes)-3))
				break
			}
			what = append(what, a.What)
		}
		b = fmt.Appendf(b, "Threads can fix (%d): %s\n", len(q.Fixes), strings.Join(what, "; "))
	}
	return string(b)
}

func briefAsk(a asks.Ask, now time.Time) string {
	age := shortAge(now.Sub(a.Since))
	switch a.Kind {
	case asks.KindQuestion:
		s := fmt.Sprintf("%s: %s (%s", clip(a.Where, 48), clip(a.What, 80), age)
		if a.Stale {
			s += ", still needed?"
		}
		return s + ")"
	case asks.KindMemory, asks.KindRecap:
		return a.What
	default:
		return fmt.Sprintf("%s (%s, %s)", clip(a.What, 80), minutes(a.Minutes), age)
	}
}

func minutes(m int) string {
	if m == 0 {
		return ""
	}
	return fmt.Sprintf("~%dm", m)
}

func init() {
	asksCmd.Flags().BoolVar(&asksJSON, "json", false, "Machine-readable output, with ask ids")
	asksCmd.Flags().BoolVar(&asksOffline, "offline", false, "Use cached PR state; don't call GitHub")
	asksRefreshCmd.Flags().BoolVar(&asksIfDue, "if-due", false, "Skip if PR state was fetched in the last hour")
	asksCmd.AddCommand(asksDropCmd, asksRefreshCmd)
	rootCmd.AddCommand(asksCmd)
}
