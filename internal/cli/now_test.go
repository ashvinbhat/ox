package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/journal"
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
