package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/journal"
	"github.com/ashvinbhat/ox/internal/park"
	"github.com/ashvinbhat/ox/internal/tracks"
)

func TestSessionTrackPrefersTitle(t *testing.T) {
	ts := []tracks.Track{
		{Name: "Reviews", Short: "review", Match: []string{"review"}},
		{Name: "ox", Short: "ox", Match: []string{"ox"}},
	}
	s := desktop.Session{
		Title: "Ox workflow and context management",
		PRs:   []desktop.PR{{Repo: "ashvinbhat/ox", Branch: "feat/ox-1.0-review-desk"}},
	}
	if got := sessionTrack(s, ts); got == nil || got.Short != "ox" {
		t.Errorf("track = %v, want ox from the title", got)
	}
	s.Title = "Untitled"
	if got := sessionTrack(s, ts); got == nil || got.Short != "review" {
		t.Errorf("track = %v, want review from the branch", got)
	}
}

func TestMentionOnceADay(t *testing.T) {
	home := t.TempDir()
	day := time.Date(2026, 10, 30, 9, 0, 0, 0, time.Local)
	v := nowView{Journal: &journal.Status{RecapDue: true, DecisionsWeek: 5}, Memory: &nowMemory{Suggestions: 1}}

	first := mentionOnce(home, v, day)
	want := []string{"the weekly recap is due (5 decisions this week)", "the weekly memory check left 1 suggestion"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("first thread of the day = %v, want %v", first, want)
	}
	if again := mentionOnce(home, v, day.Add(3*time.Hour)); again != nil {
		t.Errorf("second thread the same day = %v, want nothing", again)
	}
	if next := mentionOnce(home, v, day.AddDate(0, 0, 1)); len(next) != 2 {
		t.Errorf("next day = %v, want both again", next)
	}
	if none := mentionOnce(t.TempDir(), nowView{}, day); none != nil {
		t.Errorf("nothing due = %v", none)
	}
}

// A parked thread shows as parked with its next step, never archivable,
// until it is active again after the park.
func TestParkedThreads(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	parkedAt := now.Add(-72 * time.Hour)
	done := &desktop.Summary{Status: "completed"}
	ms := func(t time.Time) float64 { return float64(t.UnixMilli()) }
	sessions := []desktop.Session{
		{ID: "a", Title: "Left on purpose", Summary: done, LastActiveMs: ms(parkedAt.Add(time.Minute))},
		{ID: "b", Title: "Came back", Summary: done, LastActiveMs: ms(parkedAt.Add(time.Hour))},
		{ID: "c", Title: "Never parked", Summary: done, LastActiveMs: ms(parkedAt)},
	}
	parked := map[string]park.Record{
		"a": {SessionID: "a", Next: "Rebase on main", At: parkedAt},
		"b": {SessionID: "b", Next: "Old step", At: parkedAt},
	}
	got := map[string]nowThread{}
	for _, th := range threadsFrom(sessions, nil, parked, now) {
		got[th.SessionID] = th
	}
	if a := got["a"]; a.State != desktop.StateParked || a.Next != "Rebase on main" || a.Archivable {
		t.Errorf("parked thread = %+v", a)
	}
	if b := got["b"]; b.State != desktop.StateDone || b.Next != "" || !b.Archivable {
		t.Errorf("resumed thread = %+v", b)
	}
	if c := got["c"]; c.State != desktop.StateDone || !c.Archivable {
		t.Errorf("unparked thread = %+v", c)
	}
	if brief := renderBrief(nowView{At: now, Threads: threadsFrom(sessions, nil, parked, now)}); !strings.Contains(brief, "· parked 1 · done 2") {
		t.Errorf("brief counts:\n%s", brief)
	}
}
