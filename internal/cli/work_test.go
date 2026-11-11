package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/work"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

func TestEachPRBelongsToOneThread(t *testing.T) {
	sessions := []desktop.Session{
		{ID: "local_bound", Title: "pointed at it", CreatedMs: 1, PRs: []desktop.PR{
			{Repo: "ashvinbhat/ox", Number: 24, State: "MERGED"},
			{Repo: "ashvinbhat/ox", Number: 33, State: "MERGED"},
		}},
		{ID: "local_opened", Title: "opened it", CreatedMs: 2, PRs: []desktop.PR{
			{Repo: "ashvinbhat/ox", Number: 24, State: "MERGED", Own: true},
			{Repo: "ashvinbhat/ox", Number: 33, State: "MERGED"},
		}},
		{ID: "local_review", Title: "review", CreatedMs: 3, PRs: []desktop.PR{
			{Repo: "CBX1/backend", Number: 7000, State: "OPEN"},
		}},
	}
	merged := map[string]time.Time{"ox#24": time.Now(), "ox#33": time.Now()}
	threads := workThreads(sessions, nil, nil, merged, nil)

	got := map[string][]string{}
	for _, th := range threads {
		for _, p := range th.PRs {
			got[th.ID] = append(got[th.ID], p.Ref())
		}
	}
	if strings.Join(got["local_opened"], ",") != "ox#24" || strings.Join(got["local_bound"], ",") != "ox#33" {
		t.Fatalf("PRs by thread = %v; want ox#24 with the thread that opened it, ox#33 with the oldest holder", got)
	}
	if _, ok := got["local_review"]; ok {
		t.Fatalf("a thread pointed at someone else's PR got work: %v", got)
	}
}

func TestPRIsOpenOnlyWhileGitHubSaysSo(t *testing.T) {
	sessions := []desktop.Session{{ID: "local_a", PRs: []desktop.PR{
		{Repo: "CBX1/backend", Number: 1, State: "OPEN", Own: true},
		{Repo: "CBX1/backend", Number: 2, State: "OPEN", Own: true},
		{Repo: "CBX1/backend", Number: 3, State: "OPEN", Own: true},
	}}}
	open := []asks.PR{{Repo: "CBX1/backend", Number: 1}}
	merged := map[string]time.Time{"backend#2": time.Now()}
	th := workThreads(sessions, nil, open, merged, nil)[0]
	if th.PRs[0].State != work.Open || th.PRs[1].State != work.Merged || th.PRs[2].State != "" {
		t.Fatalf("states = %q, %q, %q; want open, merged, and unknown for one GitHub doesn't list",
			th.PRs[0].State, th.PRs[1].State, th.PRs[2].State)
	}
}

func TestWorkLine(t *testing.T) {
	tasks := []yokecli.Task{
		{Tags: []string{"ticket"}, Status: "in_progress"},
		{Tags: []string{"ticket"}, Status: "pending"},
		{Tags: []string{"finding"}, Status: "pending"},
		{Tags: []string{"finding"}, Status: "active"},
		{Tags: []string{"work"}, Status: "in_progress"},
	}
	if got := workLine(tasks); got != "Tickets: 2 open · Findings: 1 open" {
		t.Fatalf("workLine = %q", got)
	}
	if got := workLine(tasks[4:]); got != "" {
		t.Fatalf("workLine with no tickets or findings = %q, want none", got)
	}
}

func TestSelfViewFindsTheThreadsWork(t *testing.T) {
	tasks := []yokecli.Task{{Seq: 7, Title: "fix", Keys: []string{"pr:ox-1", "thread:local_a"}}}
	v := selfView("local_a", tasks)
	if v.Key != "thread:local_a" || v.Work == nil || v.Work.Seq != 7 {
		t.Fatalf("self = %+v", v)
	}
	if v := selfView("local_b", tasks); v.Work != nil {
		t.Fatalf("another thread's work was taken as this one's: %+v", v)
	}
}
