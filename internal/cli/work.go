package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/jobs"
	"github.com/ashvinbhat/ox/internal/tracks"
	"github.com/ashvinbhat/ox/internal/work"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

const (
	jobTickets = "tickets"
	jobWork    = "work"
)

// Tickets change in Notion a few times a day; work follows the PR state the
// hook refreshes every 10 minutes.
const (
	ticketsEvery = time.Hour
	workEvery    = 10 * time.Minute
	ticketsLimit = 2 * time.Minute
	workLimit    = 2 * time.Minute
)

var (
	workIfDue  bool
	workDryRun bool
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "The yoke tasks ox keeps for your tickets and threads",
}

var workRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Sync tickets from Notion and keep a task for every thread with a PR",
	Long: `Runs yoke sync, which brings in your current-sprint Notion tickets, then
keeps a yoke task for every desktop thread with a PR. The task is found by
the thread's key and its PRs' keys, created under the ticket a PR names,
started while a PR is open, and done once its PRs merged.

The hook runs it with --if-due: tickets hourly, work every 10 minutes. Every
yoke command it runs is logged to ~/.ox/logs/work.log. --dry-run prints the
work commands and runs nothing.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		if workDryRun {
			steps, skipped, err := planWork(cfg.Home)
			for _, s := range steps {
				fmt.Println("yoke " + shellQuote(s))
			}
			for _, s := range skipped {
				fmt.Println("skipped: " + s)
			}
			return err
		}
		tickets := func() error {
			out, err := yokecli.Sync()
			logWork(cfg.Home, "yoke sync", out, err)
			return err
		}
		keep := func() error { return keepWork(cfg.Home) }
		if workIfDue {
			_, ticketsErr := jobs.Run(cfg.Home, jobTickets, ticketsEvery, ticketsLimit, tickets)
			_, workErr := jobs.Run(cfg.Home, jobWork, workEvery, workLimit, keep)
			return errors.Join(ticketsErr, workErr)
		}
		ticketsErr := tickets()
		jobs.Note(cfg.Home, jobTickets, ticketsErr)
		workErr := keep()
		jobs.Note(cfg.Home, jobWork, workErr)
		return errors.Join(ticketsErr, workErr)
	},
}

// keepWork runs the plan. A failed command doesn't stop the rest; the first
// failure is what the job reports.
func keepWork(oxHome string) error {
	steps, skipped, err := planWork(oxHome)
	if err != nil {
		return err
	}
	for _, s := range skipped {
		logWork(oxHome, "skipped: "+s, "", nil)
	}
	var errs []error
	for _, s := range steps {
		out, err := yokecli.Exec(s...)
		logWork(oxHome, "yoke "+shellQuote(s), out, err)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// planWork reads threads, tasks and the PR cache and plans the yoke commands.
func planWork(oxHome string) ([][]string, []string, error) {
	tasks, err := yokecli.List(true, "")
	if err != nil {
		return nil, nil, err
	}
	sessions, err := desktop.List(desktop.DefaultDir(), true)
	if err != nil {
		return nil, nil, fmt.Errorf("read desktop threads: %w", err)
	}
	ts, _ := tracks.Load(oxHome)
	open, _, _ := asks.LoadPRs(oxHome)
	steps, skipped := work.Plan(workThreads(sessions, ts, open, asks.LoadMerged(oxHome), asks.LoadTitles(oxHome)), workTasks(tasks))
	return steps, skipped, nil
}

// workThreads turns threads with PRs into what work.Plan reads. A PR is
// Ashvin's when a thread opened it or GitHub lists it as his, and it belongs
// to one thread: the one that opened it, else the oldest one holding it. A
// thread pointed at another's PR doesn't take its work. The desktop app can
// lag on a PR's state and stops updating archived threads, so a PR is open
// only while GitHub lists it open, and merged once GitHub says so.
func workThreads(sessions []desktop.Session, ts []tracks.Track, open []asks.PR, merged map[string]time.Time, titles map[string]string) []work.Thread {
	isOpen := map[string]bool{}
	for _, p := range open {
		isOpen[p.Ref()] = true
	}
	owner := prOwners(sessions, func(p desktop.PR) bool {
		ref := prRef(p.Repo, p.Number)
		_, wasMerged := merged[ref]
		return !p.Dismissed && (p.Own || isOpen[ref] || wasMerged)
	})
	var threads []work.Thread
	for _, s := range sessions {
		th := work.Thread{ID: s.ID, Title: s.Title}
		if tr := sessionTrack(s, ts); tr != nil {
			th.Track = tr.Label()
		}
		for _, p := range s.PRs {
			wp := work.PR{Repo: p.Repo, Number: p.Number, Branch: p.Branch, URL: p.URL}
			ref := wp.Ref()
			if owner[ref] != s.ID {
				continue
			}
			wp.Title = titles[ref]
			_, wasMerged := merged[ref]
			switch {
			case wasMerged:
				wp.State, wp.Recent = work.Merged, true
			case p.State == "MERGED":
				wp.State = work.Merged
			case p.State == "CLOSED":
				wp.State = work.Closed
			case p.State == "OPEN" && isOpen[ref]:
				wp.State, wp.Recent = work.Open, true
			}
			th.PRs = append(th.PRs, wp)
		}
		if len(th.PRs) > 0 {
			threads = append(threads, th)
		}
	}
	return threads
}

// prOwners maps each PR mine accepts to the thread it belongs to.
func prOwners(sessions []desktop.Session, mine func(desktop.PR) bool) map[string]string {
	byAge := append([]desktop.Session(nil), sessions...)
	sort.SliceStable(byAge, func(i, j int) bool { return byAge[i].CreatedMs < byAge[j].CreatedMs })
	owner := map[string]string{}
	opened := map[string]bool{}
	for _, s := range byAge {
		for _, p := range s.PRs {
			ref := prRef(p.Repo, p.Number)
			if !mine(p) || opened[ref] {
				continue
			}
			if _, held := owner[ref]; !held || p.Own {
				owner[ref] = s.ID
				opened[ref] = p.Own
			}
		}
	}
	return owner
}

func workTasks(tasks []yokecli.Task) []work.Task {
	out := make([]work.Task, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, work.Task{ID: t.ID, Title: t.Title, Status: t.Status, Keys: t.Keys})
	}
	return out
}

func logWork(oxHome, what, out string, err error) {
	dir := filepath.Join(oxHome, "logs")
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, ferr := os.OpenFile(filepath.Join(dir, "work.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	line := time.Now().Format("2006-01-02 15:04") + " " + what
	if err != nil {
		line += " → error: " + firstLineOf(err.Error())
	} else if out != "" {
		line += " → " + firstLineOf(out)
	}
	fmt.Fprintln(f, line)
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func shellQuote(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"#:;$&|()<>*?") {
			out[i] = strconv.Quote(a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

func init() {
	workRefreshCmd.Flags().BoolVar(&workIfDue, "if-due", false, "Run each part only when due: tickets hourly, work every 10 minutes (the hook uses this)")
	workRefreshCmd.Flags().BoolVar(&workDryRun, "dry-run", false, "Print the work commands, run nothing")
	workCmd.AddCommand(workRefreshCmd)
	rootCmd.AddCommand(workCmd)
}
