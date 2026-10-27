package cli

import (
	"testing"

	"github.com/ashvinbhat/ox/internal/desktop"
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
