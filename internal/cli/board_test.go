package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/stops"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

func TestBoardNeedsAreStopsWithTheThreadsOwnWords(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	sessions := []desktop.Session{
		{ID: "local_a", CLISessionID: "cli-a2", PriorCLIIDs: []string{"cli-a1"}, LastUserMs: float64(now.Add(-3 * time.Hour).UnixMilli())},
		{ID: "local_b", CLISessionID: "cli-b", LastUserMs: float64(now.Add(-time.Hour).UnixMilli())},
	}
	log := []stops.Entry{
		{At: now.Add(-2 * time.Hour), Session: "cli-a1", Kind: "Send", Question: "Send: post the reply to Neeraj?", Passed: true},
		{At: now.Add(-90 * time.Minute), Session: "cli-a2", Question: "Commit this?"},
		{At: now.Add(-2 * time.Hour), Session: "cli-b", Kind: "Delete", Question: "Delete: drop 3 branches?", Passed: true},
	}
	q := asks.Queue{
		Waiting: []asks.Ask{
			{ID: "question:local_a", Kind: asks.KindQuestion, What: "say whether to post", Where: "Landing", Thread: "local_a", Since: now.Add(-time.Hour)},
			{ID: "question:local_b", Kind: asks.KindQuestion, What: "say whether to drop branches", Where: "Cleanup", Thread: "local_b", Since: now.Add(-time.Hour)},
			{ID: "merge:ox#45", Kind: asks.KindMerge, What: "Merge ox#45: feat: stops", Where: "ox", Thread: "local_c", Since: now},
			{ID: "review:backend#1", Kind: asks.KindReview, What: "Review backend#1 for Neeraj", Since: now},
		},
		Snoozed: []asks.Ask{
			{ID: "question:local_d", Kind: asks.KindQuestion, Thread: "local_d"},
			{ID: "memory", Kind: asks.KindMemory},
		},
	}
	b := buildBoard(q, nil, sessions, nil, log, nil, now)

	var got []string
	for _, n := range b.Needs {
		got = append(got, n.Kind+"|"+n.Title+"|"+n.What)
	}
	want := []string{
		"Send|Landing|post the reply to Neeraj?",
		"|Cleanup|say whether to drop branches",
		"Merge|ox|ox#45 is ready to merge",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("needs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if b.Snoozed != 1 {
		t.Errorf("snoozed = %d, want 1: a snoozed memory ask isn't a thread's", b.Snoozed)
	}
}

func TestBoardInProgressGroupsByTicketAndSkipsWhatsWaiting(t *testing.T) {
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	ticket := "t1"
	tasks := []yokecli.Task{
		{ID: ticket, Seq: 9, Title: "Land user in onboarding", Tags: []string{"ticket"}, Keys: []string{"CB-20605"}},
		{ID: "w1", ParentID: &ticket, Keys: []string{"thread:local_r", "pr:frontend-10019"}},
		{ID: "w2", ParentID: &ticket, Keys: []string{"thread:local_s"}},
	}
	threads := []nowThread{
		{SessionID: "local_p", Title: "Bundle split", State: desktop.StateParked, LastActive: now.Add(-14 * 24 * time.Hour)},
		{SessionID: "local_r", Title: "CB-20605 FE", State: desktop.StateInReview, PRs: []string{"frontend#10019"}, LastActive: now.Add(-2 * time.Hour)},
		{SessionID: "local_w", Title: "Background tasks", State: desktop.StateWorking, LastActive: now.Add(-time.Hour)},
		{SessionID: "local_s", Title: "CB-20605 BE", State: desktop.StateWorking, LastActive: now.Add(-3 * time.Hour)},
		{SessionID: "local_o", Title: "Other work", State: desktop.StateWorking, LastActive: now.Add(-4 * time.Hour)},
		{SessionID: "local_n", Title: "Asks something", State: desktop.StateNeedsYou, LastActive: now},
		{SessionID: "local_m", Title: "PR to merge", State: desktop.StateInReview, LastActive: now},
		{SessionID: "local_d", Title: "Finished", State: desktop.StateDone, LastActive: now},
	}
	q := asks.Queue{Waiting: []asks.Ask{{ID: "merge:ox#1", Kind: asks.KindMerge, What: "Merge ox#1: x", Thread: "local_m", Since: now}}}
	merged := map[string]time.Time{"ox#44": now.Add(-time.Hour), "ox#43": now.Add(-3 * time.Hour), "ox#40": now.Add(-30 * time.Hour)}
	b := buildBoard(q, threads, nil, tasks, nil, merged, now)

	var got []string
	for _, f := range b.InProgress {
		got = append(got, f.Ticket+"|"+f.Title+"|"+string(f.State))
	}
	want := []string{
		"|Background tasks|working",
		"CB-20605 Land user in onboarding|CB-20605 BE|working",
		"|Other work|working",
		"CB-20605 Land user in onboarding|CB-20605 FE|in review",
		"|Bundle split|parked",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("in progress:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Join(b.Landed, ",") != "ox#43,ox#44" {
		t.Errorf("landed = %v, want today's merges, oldest first", b.Landed)
	}
}
