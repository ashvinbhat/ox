package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/desktop"
)

func TestMainThreadIsRecordedInOxHome(t *testing.T) {
	home := t.TempDir()
	if got := mainThread(home); got != "" {
		t.Fatalf("no Main yet = %q", got)
	}
	if err := setMainThread(home, "local_main"); err != nil {
		t.Fatal(err)
	}
	if got := mainThread(home); got != "local_main" {
		t.Fatalf("Main = %q, want local_main", got)
	}
}

func TestRoleOfEachThread(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) float64 { return float64(now.Add(-d).UnixMilli()) }
	from := func(id string) *desktop.Spawn { return &desktop.Spawn{SessionID: id} }
	done := &desktop.Summary{Status: "completed"}
	sessions := []desktop.Session{
		{ID: "local_main", Title: "Main", Summary: done, LastActiveMs: ms(time.Hour)},
		{ID: "local_build", Title: "Build it", SpawnedFrom: from("local_main"), LastActiveMs: ms(time.Minute),
			PRs: []desktop.PR{{Repo: "ashvinbhat/ox", Number: 50, State: "OPEN"}}},
		{ID: "local_landed", Title: "Landed", SpawnedFrom: from("local_main"), Summary: &desktop.Summary{Status: "review_ready"},
			LastActiveMs: ms(time.Hour), PRs: []desktop.PR{{Repo: "CBX1/backend", Number: 7, State: "MERGED"}}},
		{ID: "local_answered", Title: "Answered", SpawnedFrom: from("local_main"), Summary: done, LastActiveMs: ms(time.Hour)},
		{ID: "local_gone", Title: "Gone", SpawnedFrom: from("local_main"), Summary: done, Archived: true},
		{ID: "local_other", Title: "Other", SpawnedFrom: from("local_else"), Summary: done},
		{ID: "local_mine", Title: "Mine", Summary: done},
	}
	threads := threadsFrom(sessions, nil, nil, "local_main", now)

	if r := roleOf("", "local_main", threads, sessions); r != nil {
		t.Errorf("no thread: role %+v, want none", r)
	}
	for self, want := range map[string]string{"local_build": roleSub, "local_other": roleWork, "local_mine": roleWork} {
		if r := roleOf(self, "local_main", threads, sessions); r == nil || r.Kind != want || r.SubThreads != nil {
			t.Errorf("%s: role %+v, want %s", self, r, want)
		}
	}
	if r := roleOf("local_build", "", threads, sessions); r.Kind != roleWork {
		t.Errorf("no Main recorded: role %s, want work", r.Kind)
	}

	r := roleOf("local_main", "local_main", threads, sessions)
	var got []string
	for _, s := range r.SubThreads {
		got = append(got, s.SessionID)
	}
	if r.Kind != roleMain || strings.Join(got, ",") != "local_build,local_landed,local_answered" {
		t.Fatalf("Main: %s with %v; want its three open sub-threads", r.Kind, got)
	}
	brief := briefRole(r)
	for _, want := range []string{
		"Role: Main. Orchestrate only: start a sub-thread for any new work.\n",
		"- Build it (local_build) · working · ox#50 open\n",
		"- Landed (local_landed) · in review · backend#7 merged\n",
		"Sign off: Landed (local_landed); Answered (local_answered)\n",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("Main's brief lacks %q:\n%s", want, brief)
		}
	}
	if got := briefRole(&nowRole{Kind: roleSub}); got != "Role: sub-thread of Main. Do the work here, own its PR, report back by ending your turn.\n" {
		t.Errorf("sub-thread line = %q", got)
	}
}

func TestSignOffNeedsTheWorkDoneAndTheThreadIdle(t *testing.T) {
	merged := []desktop.PR{{State: "MERGED"}, {State: "CLOSED"}}
	open := []desktop.PR{{State: "MERGED"}, {State: "OPEN"}}
	cases := []struct {
		name  string
		state desktop.State
		prs   []desktop.PR
		want  bool
	}{
		{"done, no PR", desktop.StateDone, nil, true},
		{"every PR merged or closed", desktop.StateInReview, merged, true},
		{"stuck, PRs merged", desktop.StateStuck, merged, true},
		{"a PR still open", desktop.StateInReview, open, false},
		{"stuck, no PR", desktop.StateStuck, nil, false},
		{"still running", desktop.StateWorking, merged, false},
		{"waiting on Ashvin", desktop.StateNeedsYou, merged, false},
		{"parked", desktop.StateParked, merged, false},
	}
	for _, c := range cases {
		if got := signsOff(c.state, c.prs); got != c.want {
			t.Errorf("%s: sign off = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMainIsNeverArchivableInReviewOrListedWithPRs(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	idle := float64(now.Add(-72 * time.Hour).UnixMilli())
	sessions := []desktop.Session{
		{ID: "local_main", Summary: &desktop.Summary{Status: "completed"}, LastActiveMs: idle,
			PRs: []desktop.PR{{Repo: "ashvinbhat/ox", Number: 49, State: "OPEN"}}},
		{ID: "local_old", Summary: &desktop.Summary{Status: "completed"}, LastActiveMs: idle},
	}
	threads := threadsFrom(sessions, nil, nil, "local_main", now)
	if m := threads[0]; m.Archivable || m.PRs != nil || m.State != desktop.StateDone {
		t.Errorf("Main = %+v; want done, not archivable, no PRs", m)
	}
	sessions[0].Summary.Status = "review_ready"
	if m := threadsFrom(sessions, nil, nil, "local_main", now)[0]; m.State != desktop.StateDone {
		t.Errorf("Main marked review-ready = %s; want done, never in review", m.State)
	}
	if !threads[1].Archivable {
		t.Error("another done thread idle 3 days should stay archivable")
	}
}

func TestHookSelfFallsBackToTheAppsThreadID(t *testing.T) {
	t.Setenv(desktop.HostEnv, "local_here")
	if got := hookSelf(""); got != "local_here" {
		t.Errorf("hookSelf = %q, want the thread id from the env", got)
	}
}
