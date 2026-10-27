package ghreview

import (
	"testing"
	"time"
)

func TestStatus(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 10, n, 12, 0, 0, 0, time.UTC) }
	round := ReviewRecord{ReviewedAt: day(22), HeadSHA: "aaa"}
	cases := []struct {
		name string
		s    State
		want string
	}{
		{"asked, never reviewed", State{Asks: []Ask{{From: "Neeraj", Where: "slack-dm", At: day(23)}}}, QueueToReview},
		{"asked again after a round", State{Head: "aaa", History: []ReviewRecord{round}, Asks: []Ask{{From: "Neeraj", At: day(21)}, {From: "Neeraj", At: day(24)}}}, QueueReReview},
		{"new commits since the round", State{Head: "bbb", History: []ReviewRecord{round}}, QueueReReview},
		{"reviewed, nothing new", State{Head: "aaa", History: []ReviewRecord{round}, Asks: []Ask{{From: "Neeraj", At: day(21)}}}, QueueWaiting},
		{"merged", State{PRState: "MERGED", Asks: []Ask{{From: "Neeraj", At: day(23)}}}, QueueClosed},
	}
	for _, c := range cases {
		if got, _, _ := c.s.Status(); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOpenFindingsSpanRoundsAndDropAddressed(t *testing.T) {
	s := State{History: []ReviewRecord{
		{Findings: []Finding{{Ref: "F1"}, {Ref: "F2"}}},
		{Findings: []Finding{{Ref: "F3"}}, Addressing: []Addressing{{Ref: "F1", Status: AddressingAddressed}, {Ref: "F2", Status: AddressingPartial}}},
	}}
	var refs []string
	for _, f := range s.OpenFindings() {
		refs = append(refs, f.Ref)
	}
	if len(refs) != 2 || refs[0] != "F2" || refs[1] != "F3" {
		t.Fatalf("open = %v, want [F2 F3]", refs)
	}
	if n := s.findingCount(); n != 3 {
		t.Fatalf("findingCount = %d, want 3 (next ref F4)", n)
	}
}

func TestAddAskIgnoresSameLink(t *testing.T) {
	var s State
	at := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	if !s.AddAsk(Ask{From: "Neeraj", Link: "https://slack/p1", At: at}) {
		t.Fatal("first ask not added")
	}
	if s.AddAsk(Ask{From: "Neeraj", Link: "https://slack/p1", At: at.Add(time.Hour)}) {
		t.Fatal("same link added twice")
	}
	if !s.AddAsk(Ask{From: "Neeraj", Link: "https://slack/p2", At: at.Add(time.Hour)}) {
		t.Fatal("new link not added")
	}
	if got := s.LatestAsk().Link; got != "https://slack/p2" {
		t.Fatalf("latest = %s", got)
	}
}
