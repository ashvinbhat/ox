package asks

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestBuildRanksAndSplits(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	in := Inputs{
		Threads: []Thread{
			{ID: "local_a", Title: "A", Ask: "Ship it?", Since: ago(80), PRs: []string{"frontend#1"}, Track: "durable"},
			{ID: "local_b", Title: "B", Ask: "Which option?", Since: ago(2)},
			{ID: "local_c", Title: "C", Since: ago(1), PRs: []string{"frontend#2"}},
			{ID: "local_d", Title: "D", Ask: "Old one", Since: ago(300), Archived: true},
		},
		Reviews: []Review{{Ref: "backend#9", From: "Neeraj Gopalakrishnan", Since: ago(20)}},
		PRs: []PR{
			{Repo: "CBX1/frontend", Number: 1, Mergeable: "CONFLICTING", Checks: ChecksFailing, UpdatedAt: ago(5)},
			{Repo: "CBX1/frontend", Number: 2, Title: "Valkey", Mergeable: "MERGEABLE", Review: "APPROVED", Checks: ChecksPassing, UpdatedAt: ago(3)},
			{Repo: "CBX1/infra", Number: 3, Mergeable: "MERGEABLE", Review: "REVIEW_REQUIRED", Checks: ChecksPassing, UpdatedAt: ago(3)},
			{Repo: "ashvinbhat/ox", Number: 4, Title: "slice", Mergeable: "MERGEABLE", Checks: ChecksNone, Mine: true, UpdatedAt: ago(4)},
		},
		MemorySuggestions: 6,
		RecapDue:          true,
		DecisionsWeek:     1,
	}
	q := Build(in, map[string]time.Time{}, now)

	var ids []string
	for _, a := range q.Waiting {
		ids = append(ids, a.ID)
	}
	want := []string{"question:local_a", "question:local_b", "review:backend#9", "merge:ox#4", "merge:frontend#2", "memory", "recap"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("waiting = %v\nwant      %v", ids, want)
	}
	if !q.Waiting[0].Stale || q.Waiting[1].Stale {
		t.Error("only the question older than 3 days is stale")
	}
	if q.Waiting[2].What != "Review backend#9 for Neeraj" {
		t.Errorf("review = %q", q.Waiting[2].What)
	}
	if q.Waiting[4].Where != "C" {
		t.Errorf("merge thread = %q, want C", q.Waiting[4].Where)
	}
	if q.Waiting[5].Minutes != 10 || q.Waiting[5].What != "Memory: 6 suggestions (/memory-check)" {
		t.Errorf("memory = %+v", q.Waiting[5])
	}
	if len(q.Fixes) != 1 || q.Fixes[0].What != "frontend#1: merge conflict, CI failing" || q.Fixes[0].Where != "A" ||
		q.Fixes[0].Thread != "local_a" || q.Fixes[0].Track != "durable" {
		t.Errorf("fixes = %+v", q.Fixes)
	}
}

func TestDroppedComesBackWhenItChanges(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	in := Inputs{Threads: []Thread{{ID: "local_a", Title: "A", Ask: "?", Since: now.Add(-time.Hour)}}}
	if q := Build(in, map[string]time.Time{"question:local_a": now.Add(-30 * time.Minute)}, now); len(q.Waiting) != 0 || q.Dropped != 1 {
		t.Errorf("dropped after it was asked: %+v", q)
	}
	if q := Build(in, map[string]time.Time{"question:local_a": now.Add(-2 * time.Hour)}, now); len(q.Waiting) != 1 {
		t.Errorf("asked again after the drop, so it's back: %+v", q)
	}
}

func TestADropIsASnoozeForADay(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	in := Inputs{Threads: []Thread{{ID: "local_a", Title: "A", Ask: "?", Since: now.Add(-48 * time.Hour)}}}
	q := Build(in, map[string]time.Time{"question:local_a": now.Add(-time.Hour)}, now)
	if len(q.Waiting) != 0 || len(q.Snoozed) != 1 || q.Snoozed[0].ID != "question:local_a" {
		t.Errorf("snoozed an hour ago: %+v", q)
	}
	if q := Build(in, map[string]time.Time{"question:local_a": now.Add(-25 * time.Hour)}, now); len(q.Waiting) != 1 || len(q.Snoozed) != 0 {
		t.Errorf("snoozed 25h ago and still waiting, so it's back: %+v", q)
	}
}

func TestPRWaitsOnNobodyWhileInReview(t *testing.T) {
	for _, p := range []PR{
		{Repo: "CBX1/frontend", Number: 1, Mergeable: "MERGEABLE", Review: "APPROVED", Checks: ChecksPending},
		{Repo: "CBX1/frontend", Number: 2, Mergeable: "MERGEABLE", Review: "APPROVED", Checks: ChecksPassing, Draft: true},
		{Repo: "CBX1/frontend", Number: 3, Mergeable: "MERGEABLE", Review: "", Checks: ChecksPassing},
	} {
		if a, ok := prAsk(p, Thread{}); ok {
			t.Errorf("%s: got %+v, want nothing", p.Ref(), a)
		}
	}
}

func TestChecksState(t *testing.T) {
	cases := map[string]string{
		`[]`: ChecksNone,
		`[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"CANCELLED"}]`: ChecksPassing,
		`[{"status":"IN_PROGRESS","conclusion":""},{"status":"COMPLETED","conclusion":"SUCCESS"}]`:        ChecksPending,
		`[{"status":"IN_PROGRESS"},{"status":"COMPLETED","conclusion":"FAILURE"}]`:                        ChecksFailing,
		`[{"state":"PENDING"}]`: ChecksPending,
		`[{"state":"ERROR"}]`:   ChecksFailing,
	}
	for raw, want := range cases {
		if got := checksState(json.RawMessage(raw)); got != want {
			t.Errorf("%s: %s, want %s", raw, got, want)
		}
	}
}

func TestQuestionAskedBeforeItsPRMergedIsFlagged(t *testing.T) {
	now := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC)
	asked := now.Add(-3 * time.Hour)
	threads := []Thread{
		{ID: "a", Title: "Hook in Go", Ask: "Turn on Auto-fix?", Since: asked, PRs: []string{"ox#35"}},
		{ID: "b", Title: "Budget", Ask: "Build the four?", Since: asked, PRs: []string{"ox#34"}},
		{ID: "c", Title: "No PR", Ask: "Which first?", Since: asked},
	}
	merged := map[string]time.Time{
		"ox#35": now.Add(-time.Hour),     // after the question
		"ox#34": now.Add(-5 * time.Hour), // before it
	}
	q := Build(Inputs{Threads: threads, Merged: merged}, nil, now)
	got := map[string]string{}
	for _, a := range q.Waiting {
		got[a.Thread] = a.Settled
	}
	want := map[string]string{"a": "ox#35 merged since", "b": "", "c": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("settled = %v, want %v", got, want)
	}
}

func TestMergedPRsRoundTripTheCache(t *testing.T) {
	home := t.TempDir()
	merged, err := parseMerged(`[{"closedAt":"2026-11-05T10:59:04Z","number":35,"title":"feat: loud job failures","repository":{"name":"ox","nameWithOwner":"ashvinbhat/ox"}}]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := SavePRs(home, nil, merged, time.Now()); err != nil {
		t.Fatal(err)
	}
	at := LoadMerged(home)["ox#35"]
	if want := time.Date(2026, 11, 5, 10, 59, 4, 0, time.UTC); !at.Equal(want) {
		t.Errorf("ox#35 merged at %v, want %v", at, want)
	}
	if got := LoadTitles(home)["ox#35"]; got != "feat: loud job failures" {
		t.Errorf("ox#35's title = %q", got)
	}
}
