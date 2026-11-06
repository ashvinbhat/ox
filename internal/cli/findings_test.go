package cli

import (
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/tracks"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

func ptr(s string) *string { return &s }

func TestFindingsGroupByTheTicketTheirWorkIsUnder(t *testing.T) {
	day := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	all := []yokecli.Task{
		{ID: "tk", Title: "Video transcription", Status: "in_progress", Tags: []string{"ticket"}, Keys: []string{"CB-18304"}},
		{ID: "w", Title: "transcripts thread", Status: "done", Tags: []string{"work"}, ParentID: ptr("tk")},
		{ID: "f1", Status: "pending", Tags: []string{"finding"}, FoundFromID: ptr("w"), CreatedAt: day},
		{ID: "f2", Status: "pending", Tags: []string{"finding"}, ParentID: ptr("tk"), CreatedAt: day.Add(time.Hour)},
		{ID: "f3", Status: "active", Tags: []string{"finding"}, ParentID: ptr("tk")},
		{ID: "f4", Status: "pending", Tags: []string{"finding", "ox"}, CreatedAt: day},
	}
	ts := []tracks.Track{{Name: "ox", Short: "ox"}}
	got := findingGroups(all, ts)
	if len(got) != 2 {
		t.Fatalf("groups = %+v, want the ticket's and the track's", got)
	}
	tk := got[0]
	if tk.ID != "findings:CB-18304" || tk.Title != "CB-18304 Video transcription" || tk.Count != 2 || !tk.Since.Equal(day.Add(time.Hour)) {
		t.Fatalf("ticket group = %+v", tk)
	}
	if got[1].ID != "findings:track:ox" || got[1].Count != 1 {
		t.Fatalf("track group = %+v", got[1])
	}

	markDroppedFindings(got, map[string]time.Time{"findings:CB-18304": day.Add(30 * time.Minute)})
	if got[0].Dropped {
		t.Fatal("a finding newer than the clear didn't bring the line back")
	}
	markDroppedFindings(got, map[string]time.Time{"findings:CB-18304": day.Add(2 * time.Hour)})
	if !got[0].Dropped {
		t.Fatal("a cleared line with nothing new came back")
	}
}
