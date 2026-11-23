package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/desktop"
)

func TestRoleOfEachThread(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) float64 { return float64(now.Add(-d).UnixMilli()) }
	from := func(id string) *desktop.Spawn { return &desktop.Spawn{SessionID: id} }
	done := &desktop.Summary{Status: "completed"}
	sessions := []desktop.Session{
		{ID: "local_ox", Title: "ox", Summary: &desktop.Summary{Status: "review_ready"}, LastActiveMs: ms(time.Hour),
			PRs: []desktop.PR{{Repo: "ashvinbhat/ox", Number: 48, State: "MERGED", Own: true}}},
		{ID: "local_build", Title: "Build it", SpawnedFrom: from("local_ox"), LastActiveMs: ms(time.Minute),
			PRs: []desktop.PR{{Repo: "ashvinbhat/ox", Number: 51, State: "OPEN", Own: true}}},
		{ID: "local_landed", Title: "Landed", SpawnedFrom: from("local_ox"), Summary: &desktop.Summary{Status: "review_ready"},
			LastActiveMs: ms(time.Hour), PRs: []desktop.PR{{Repo: "CBX1/backend", Number: 7, State: "MERGED", Own: true}}},
		{ID: "local_answered", Title: "Answered", SpawnedFrom: from("local_ox"), Summary: done, LastActiveMs: ms(time.Hour)},
		{ID: "local_gone", Title: "Gone", SpawnedFrom: from("local_ox"), Summary: done, Archived: true},
		{ID: "local_side", Title: "Side chat", SpawnedFrom: &desktop.Spawn{SessionID: "local_ox", UserFork: true, Btw: true}, Summary: done},
		{ID: "local_orphan", Title: "Orphan", SpawnedFrom: from("local_gone_parent"), Summary: done},
		{ID: "local_mine", Title: "Mine", Summary: done},
	}
	threads := threadsFrom(sessions, nil, nil, now)

	if r := roleOf("", threads, sessions); r != nil {
		t.Errorf("no thread: role %+v, want none", r)
	}

	sub := roleOf("local_build", threads, sessions)
	if sub == nil || sub.Kind != roleSub || sub.Parent != "local_ox" || sub.SubThreads != nil {
		t.Fatalf("a thread ox started: role %+v, want a sub-thread of local_ox", sub)
	}
	if got := briefRole(sub); got != "Role: sub-thread of \"ox\". Do the work here and own its PR; report back to that thread by ending your turn.\n" {
		t.Errorf("sub-thread line = %q", got)
	}
	if r := roleOf("local_orphan", threads, sessions); r.Kind != roleSub || r.ParentTitle != "local_gone_parent" {
		t.Errorf("parent not listed: role %+v, want a sub-thread named by the parent's id", r)
	}

	for _, self := range []string{"local_side", "local_mine"} {
		r := roleOf(self, threads, sessions)
		if r == nil || r.Kind != roleDiscussion || r.SubThreads != nil {
			t.Errorf("%s: role %+v, want a discussion thread with no sub-threads", self, r)
		}
		if got := briefRole(r); got != "Role: discussion thread. Talk and decide here; work that changes a repo or ends in a PR goes to a sub-thread you start.\n" {
			t.Errorf("%s: discussion line = %q", self, got)
		}
	}

	r := roleOf("local_ox", threads, sessions)
	var got []string
	for _, s := range r.SubThreads {
		got = append(got, s.SessionID)
	}
	if r.Kind != roleDiscussion || strings.Join(got, ",") != "local_build,local_landed,local_answered" {
		t.Fatalf("ox: %s with %v; want its three open sub-threads, no side chat", r.Kind, got)
	}
	brief := briefRole(r)
	for _, want := range []string{
		"Role: discussion thread. Talk and decide here;",
		"Sub-threads (3):\n",
		"- Build it (local_build) · working · ox#51 open\n",
		"- Landed (local_landed) · done · backend#7 merged\n",
		"Sign off: Landed (local_landed); Answered (local_answered)\n",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("ox's brief lacks %q:\n%s", want, brief)
		}
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

func TestAThreadWithOpenSubThreadsIsNeverArchivable(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	idle := float64(now.Add(-72 * time.Hour).UnixMilli())
	done := func() *desktop.Summary { return &desktop.Summary{Status: "completed"} }
	sessions := []desktop.Session{
		{ID: "local_ox", Summary: done(), LastActiveMs: idle},
		{ID: "local_sub", SpawnedFrom: &desktop.Spawn{SessionID: "local_ox"}, Summary: done(), LastActiveMs: idle},
		{ID: "local_asked", Summary: done(), LastActiveMs: idle},
		{ID: "local_fork", SpawnedFrom: &desktop.Spawn{SessionID: "local_asked", UserFork: true}, Summary: done(), LastActiveMs: idle},
	}
	archivable := func() map[string]bool {
		out := map[string]bool{}
		for _, th := range threadsFrom(sessions, nil, nil, now) {
			out[th.SessionID] = th.Archivable
		}
		return out
	}
	got := archivable()
	if got["local_ox"] {
		t.Error("ox has an open sub-thread: it should not be archivable")
	}
	if !got["local_sub"] || !got["local_asked"] {
		t.Errorf("done threads idle 3 days with no open sub-thread should be archivable: %v", got)
	}
	sessions[1].Archived = true
	if !archivable()["local_ox"] {
		t.Error("ox's only sub-thread is archived: ox should be archivable")
	}
}

func TestHookSelfFallsBackToTheAppsThreadID(t *testing.T) {
	t.Setenv(desktop.HostEnv, "local_here")
	if got := hookSelf(""); got != "local_here" {
		t.Errorf("hookSelf = %q, want the thread id from the env", got)
	}
}
