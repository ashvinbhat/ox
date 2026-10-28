package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/ghreview"
	"github.com/ashvinbhat/ox/internal/journal"
	"github.com/ashvinbhat/ox/internal/memcheck"
	"github.com/ashvinbhat/ox/internal/tracks"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

var (
	nowBrief bool
	nowJSON  bool
	nowTrack string
	nowAll   bool
	// nowMention adds the once-a-day reminders to the brief; only the
	// ox-brief hook passes it, so a manual run doesn't use up the day's.
	nowMention bool
)

var nowCmd = &cobra.Command{
	Use:   "now",
	Short: "Every thread by state and track, plus tasks and open PRs",
	Long: `One view of your Claude Code desktop threads, grouped by what they need
from you: needs you, in review, working, stuck, done. Each thread carries its
track from ~/.ox/tracks.yaml. Below them: yoke tasks in progress and your open
PRs that no thread owns.

--brief prints the short version every new thread gets at start.`,
	Args: cobra.NoArgs,
	RunE: runNow,
}

type nowThread struct {
	SessionID  string        `json:"sessionId"`
	Title      string        `json:"title"`
	Track      string        `json:"track,omitempty"`
	State      desktop.State `json:"state"`
	Ask        string        `json:"ask,omitempty"`
	Recap      string        `json:"recap,omitempty"`
	PRs        []string      `json:"prs,omitempty"`
	LastActive time.Time     `json:"lastActive"`
	Archivable bool          `json:"archivable,omitempty"`
}

type nowPR struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// nowMemory is what the weekly memory audit left for Ashvin to look at.
type nowMemory struct {
	Suggestions int       `json:"suggestions"`
	AuditedAt   time.Time `json:"auditedAt"`
}

type nowView struct {
	At      time.Time            `json:"at"`
	Threads []nowThread          `json:"threads"`
	Reviews []ghreview.QueueItem `json:"reviews,omitempty"`
	Memory  *nowMemory           `json:"memory,omitempty"`
	Journal *journal.Status      `json:"journal,omitempty"`
	Asks    *asks.Queue          `json:"asks,omitempty"`
	Mention []string             `json:"mention,omitempty"`
	Tasks   []yokecli.Task       `json:"tasks,omitempty"`
	PRs     []nowPR              `json:"prsWithoutThread,omitempty"`
}

func runNow(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	now := time.Now()

	ts, err := tracks.Load(cfg.Home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}
	sessions, err := desktop.List(desktop.DefaultDir(), nowAll)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: read desktop threads: %v\n", err)
	}

	view := nowView{At: now, Threads: threadsFrom(sessions, ts, now), Reviews: reviewsToDo(cfg.Home)}
	if r, err := memcheck.Pending(cfg.Home); err == nil && len(r.Suggestions) > 0 {
		view.Memory = &nowMemory{Suggestions: len(r.Suggestions), AuditedAt: r.At}
	}
	if st, err := journalStatus(cfg.Home, now); err == nil {
		view.Journal = &st
	}
	// yoke missing or failing only costs the tasks section.
	if tasks, err := yokecli.List(false, ""); err == nil {
		view.Tasks = tasks
	}
	if !nowBrief {
		view.PRs = prsWithoutThread(sessions)
	}
	if nowTrack != "" {
		view = filterTrack(view, ts, nowTrack)
	}
	// The brief must not wait on GitHub: it reads the PR cache the hook keeps.
	q := collectAsks(cfg.Home, view.Threads, view.Reviews, now, !nowBrief)
	if nowTrack != "" {
		q = trackAsks(q, view.Threads)
	}
	view.Asks = &q
	if nowBrief && nowMention {
		view.Mention = mentionOnce(cfg.Home, view, now)
	}

	switch {
	case nowJSON:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	case nowBrief:
		fmt.Print(renderBrief(view))
	default:
		renderFull(os.Stdout, view)
	}
	return nil
}

func threadsFrom(sessions []desktop.Session, ts []tracks.Track, now time.Time) []nowThread {
	out := make([]nowThread, 0, len(sessions))
	for _, s := range sessions {
		var prs []string
		for _, p := range s.OpenPRs() {
			prs = append(prs, prRef(p.Repo, p.Number))
		}
		t := nowThread{
			SessionID:  s.ID,
			Title:      s.Title,
			State:      s.State(now),
			Ask:        s.Ask(),
			Recap:      s.Recap(),
			PRs:        prs,
			LastActive: s.LastActive(),
			Archivable: s.Archivable(now),
		}
		if tr := sessionTrack(s, ts); tr != nil {
			t.Track = tr.Label()
		}
		out = append(out, t)
	}
	return out
}

// mentionOnce returns the reminders Ashvin should hear today: a due recap and
// memory suggestions. He never sees the brief and doesn't run commands, so
// the first thread of the day says them in words. Later threads that day get
// nothing.
func mentionOnce(oxHome string, v nowView, now time.Time) []string {
	var items []string
	if j := v.Journal; j != nil && j.RecapDue {
		items = append(items, fmt.Sprintf("the weekly recap is due (%s this week)", countOf(j.DecisionsWeek, "decision")))
	}
	if v.Memory != nil {
		items = append(items, fmt.Sprintf("the weekly memory check left %s", countSuggestions(v.Memory.Suggestions)))
	}
	if len(items) == 0 {
		return nil
	}
	stamp := filepath.Join(asks.Dir(oxHome), "mentioned")
	today := now.Format("2006-01-02")
	if b, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(b)) == today {
		return nil
	}
	if os.MkdirAll(filepath.Dir(stamp), 0o755) != nil || os.WriteFile(stamp, []byte(today+"\n"), 0o644) != nil {
		return nil
	}
	return items
}

// trackAsks keeps the asks that belong to the filtered threads: their
// questions and their PRs. Reviews follow the review filter already applied.
func trackAsks(q asks.Queue, threads []nowThread) asks.Queue {
	titles := map[string]bool{}
	for _, t := range threads {
		titles[t.Title] = true
	}
	keep := func(list []asks.Ask) []asks.Ask {
		var out []asks.Ask
		for _, a := range list {
			if a.Kind == asks.KindReview || titles[a.Where] {
				out = append(out, a)
			}
		}
		return out
	}
	q.Waiting, q.Fixes = keep(q.Waiting), keep(q.Fixes)
	return q
}

// sessionTrack matches a thread to a track by its title, falling back to its
// PRs' repos and branches. The title goes first: a thread that once opened a
// review-desk PR is still about ox, not about reviews.
func sessionTrack(s desktop.Session, ts []tracks.Track) *tracks.Track {
	if t := tracks.Of(ts, s.Title); t != nil {
		return t
	}
	var texts []string
	for _, p := range s.PRs {
		texts = append(texts, p.Repo, p.Branch)
	}
	return tracks.Of(ts, texts...)
}

// reviewsToDo reads the review ledger without calling GitHub, so the brief
// stays instant; `ox reviews` refreshes PR state.
func reviewsToDo(oxHome string) []ghreview.QueueItem {
	items, err := ghreview.Queue(oxHome, false)
	if err != nil {
		return nil
	}
	var todo []ghreview.QueueItem
	for _, it := range items {
		if it.Status == ghreview.QueueToReview || it.Status == ghreview.QueueReReview {
			todo = append(todo, it)
		}
	}
	return todo
}

func filterTrack(v nowView, ts []tracks.Track, label string) nowView {
	var threads []nowThread
	for _, t := range v.Threads {
		if t.Track == label {
			threads = append(threads, t)
		}
	}
	var tasks []yokecli.Task
	for _, task := range v.Tasks {
		if tr := tracks.Of(ts, append([]string{task.Title}, task.Tags...)...); tr != nil && tr.Label() == label {
			tasks = append(tasks, task)
		}
	}
	var prs []nowPR
	for _, p := range v.PRs {
		if tr := tracks.Of(ts, p.Ref, p.Title); tr != nil && tr.Label() == label {
			prs = append(prs, p)
		}
	}
	var reviews []ghreview.QueueItem
	for _, r := range v.Reviews {
		if tr := tracks.Of(ts, r.Ref, r.Title); tr != nil && tr.Label() == label {
			reviews = append(reviews, r)
		}
	}
	v.Threads, v.Tasks, v.PRs, v.Reviews = threads, tasks, prs, reviews
	return v
}

// prsWithoutThread lists the user's open PRs that no desktop thread opened,
// so work started elsewhere still shows up. A slow or missing gh just drops
// the section.
func prsWithoutThread(sessions []desktop.Session) []nowPR {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "search", "prs", "--author=@me", "--state=open",
		"--limit", "50", "--json", "repository,number,title,url,updatedAt").Output()
	if err != nil {
		return nil
	}
	var found []struct {
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		URL       string    `json:"url"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	if json.Unmarshal(out, &found) != nil {
		return nil
	}
	owned := map[string]bool{}
	for _, s := range sessions {
		for _, p := range s.PRs {
			owned[prRef(p.Repo, p.Number)] = true
		}
	}
	var prs []nowPR
	for _, f := range found {
		ref := prRef(f.Repository.NameWithOwner, f.Number)
		if owned[ref] {
			continue
		}
		prs = append(prs, nowPR{Ref: ref, Title: f.Title, URL: f.URL, UpdatedAt: f.UpdatedAt})
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].UpdatedAt.After(prs[j].UpdatedAt) })
	return prs
}

// prRef shortens "CBX1/frontend" + 9670 to "frontend#9670".
func prRef(repo string, n int) string {
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		repo = repo[i+1:]
	}
	return fmt.Sprintf("%s#%d", repo, n)
}

// byState groups threads. Needs-you threads come oldest first, since the one
// waiting longest is the one to answer; the rest stay most recent first.
func byState(threads []nowThread) map[desktop.State][]nowThread {
	groups := map[desktop.State][]nowThread{}
	for _, t := range threads {
		groups[t.State] = append(groups[t.State], t)
	}
	needs := groups[desktop.StateNeedsYou]
	sort.SliceStable(needs, func(i, j int) bool { return needs[i].LastActive.Before(needs[j].LastActive) })
	return groups
}

func renderBrief(v nowView) string {
	var b strings.Builder
	groups := byState(v.Threads)
	fmt.Fprintf(&b, "## ox now · %s\n", v.At.Format("Mon 2 Jan 15:04"))

	if v.Asks != nil {
		b.WriteString(briefAsks(*v.Asks, v.At))
	}
	if len(v.Mention) > 0 {
		fmt.Fprintf(&b, "Mention once, as a plain offer at the end of your first reply: %s.\n", strings.Join(v.Mention, "; "))
	}

	if review := groups[desktop.StateInReview]; len(review) > 0 {
		var refs []string
		for _, t := range review {
			if len(t.PRs) > 0 {
				refs = append(refs, t.PRs...)
			} else {
				refs = append(refs, clip(t.Title, 30))
			}
		}
		if len(refs) > 4 {
			refs = append(refs[:4], fmt.Sprintf("+%d", len(refs)-4))
		}
		fmt.Fprintf(&b, "In review (%d): %s\n", len(review), strings.Join(refs, ", "))
	}

	done := groups[desktop.StateDone]
	line := fmt.Sprintf("Working %d · stuck %d · done %d", len(groups[desktop.StateWorking]), len(groups[desktop.StateStuck]), len(done))
	if n := countArchivable(done); n > 0 {
		line += fmt.Sprintf(" (%d can be archived: /tidy)", n)
	}
	b.WriteString(line + "\n")

	if t := taskLine(v.Tasks); t != "" {
		b.WriteString(t + "\n")
	}
	b.WriteString("Full view: `ox now`.\n")
	return b.String()
}

func renderFull(w io.Writer, v nowView) {
	groups := byState(v.Threads)
	fmt.Fprintf(w, "ox now · %s · %d threads\n", v.At.Format("Mon 2 Jan 15:04"), len(v.Threads))
	if v.Asks != nil {
		fmt.Fprintln(w)
		renderAsks(w, *v.Asks, v.At)
	}

	for _, st := range desktop.States {
		threads := groups[st]
		if len(threads) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s · %d\n", strings.ToUpper(string(st)), len(threads))
		shown := threads
		if st == desktop.StateDone && len(shown) > 5 {
			shown = shown[:5]
		}
		for _, t := range shown {
			fmt.Fprintf(w, "  %-8s %-44s %4s  %s\n", clip(t.Track, 8), clip(t.Title, 44), shortAge(v.At.Sub(t.LastActive)), detail(t, v.At))
		}
		if hidden := len(threads) - len(shown); hidden > 0 {
			fmt.Fprintf(w, "  +%d older\n", hidden)
		}
	}

	if j := v.Journal; j != nil && (j.DecisionsWeek > 0 || j.OwnItToday > 0) {
		fmt.Fprintf(w, "\nJOURNAL · today %s, own-it %d of %d · last 7 days %s\n",
			countOf(j.DecisionsToday, "decision"), j.OwnItToday, journal.OwnItPerDay, countOf(j.DecisionsWeek, "decision"))
		next := "  `ox journal` reads today, `--days 7` the week"
		if j.RecapDue {
			next += "; recap due (/recap)"
		}
		fmt.Fprintln(w, next)
	}

	if len(v.Tasks) > 0 {
		fmt.Fprintf(w, "\nTASKS\n")
		for _, t := range v.Tasks {
			if isStarted(t) {
				fmt.Fprintf(w, "  #%-4d %-12s %s\n", t.Seq, strings.ReplaceAll(t.Status, "_", " "), clip(t.Title, 70))
			}
		}
		fmt.Fprintf(w, "  %s\n", taskLine(v.Tasks))
	}

	var recent []nowPR
	for _, p := range v.PRs {
		if v.At.Sub(p.UpdatedAt) <= quietPR {
			recent = append(recent, p)
		}
	}
	if len(recent) > 0 {
		fmt.Fprintf(w, "\nPRS WITHOUT A THREAD · %d\n", len(recent))
		for _, p := range recent {
			fmt.Fprintf(w, "  %-28s %4s  %s\n", clip(p.Ref, 28), shortAge(v.At.Sub(p.UpdatedAt)), clip(p.Title, 60))
		}
	}
	if quiet := len(v.PRs) - len(recent); quiet > 0 {
		fmt.Fprintf(w, "\n%d more open PR(s) untouched for 14d+ (see --json)\n", quiet)
	}
}

// Open PRs nobody has touched in two weeks are parked, not current work.
const quietPR = 14 * 24 * time.Hour

func detail(t nowThread, now time.Time) string {
	switch t.State {
	case desktop.StateNeedsYou:
		return "asks: " + clip(askOrRecap(t), 70)
	case desktop.StateInReview:
		if len(t.PRs) > 0 {
			return strings.Join(t.PRs, ", ") + "  " + clip(t.Recap, 50)
		}
		return clip(t.Recap, 70)
	case desktop.StateStuck:
		return "no activity for " + shortAge(now.Sub(t.LastActive))
	case desktop.StateDone:
		if t.Archivable {
			return "archive?  " + clip(t.Recap, 60)
		}
	}
	return clip(t.Recap, 70)
}

func askOrRecap(t nowThread) string {
	if t.Ask != "" {
		return t.Ask
	}
	if t.Recap != "" {
		return t.Recap
	}
	return "waiting on you"
}

func countArchivable(threads []nowThread) int {
	n := 0
	for _, t := range threads {
		if t.Archivable {
			n++
		}
	}
	return n
}

func isStarted(t yokecli.Task) bool {
	return t.Status == yokecli.StatusInProgress || t.Status == yokecli.StatusActive
}

// taskLine summarises yoke: what is started, and how many tasks are open.
func taskLine(tasks []yokecli.Task) string {
	if len(tasks) == 0 {
		return ""
	}
	var started []string
	for _, t := range tasks {
		if isStarted(t) {
			started = append(started, fmt.Sprintf("#%d", t.Seq))
		}
	}
	if len(started) == 0 {
		return fmt.Sprintf("yoke: %d open", len(tasks))
	}
	return fmt.Sprintf("yoke: %s in progress · %d open", strings.Join(started, " "), len(tasks))
}

func shortAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// clip shortens s to n runes on one line.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func init() {
	nowCmd.Flags().BoolVar(&nowBrief, "brief", false, "Short version for the start of a thread")
	nowCmd.Flags().BoolVar(&nowMention, "mention", false, "With --brief: add the day's reminders, once a day (the ox-brief hook)")
	nowCmd.Flags().BoolVar(&nowJSON, "json", false, "Machine-readable output")
	nowCmd.Flags().StringVar(&nowTrack, "track", "", "Only this track (its short label)")
	nowCmd.Flags().BoolVar(&nowAll, "all", false, "Include archived threads")
	rootCmd.AddCommand(nowCmd)
}
