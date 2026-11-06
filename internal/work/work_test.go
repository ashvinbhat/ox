package work

import (
	"reflect"
	"strconv"
	"testing"
)

func pr(n int, state string, recent bool) PR {
	return PR{Repo: "CBX1/backend", Number: n, Title: "CB-20381: member timeout", State: state, Recent: recent,
		URL: "https://github.com/CBX1/backend/pull/" + strconv.Itoa(n)}
}

func TestNewThreadWithOpenPRGetsStartedWork(t *testing.T) {
	ticket := Task{ID: "t1", Title: "DM2 migration", Status: "done", Keys: []string{"CB-20381"}}
	th := Thread{ID: "local_a", Title: "Descope migration fix", Track: "dm2", PRs: []PR{pr(6950, Open, true)}}

	steps := Plan([]Thread{th}, []Task{ticket})
	want := [][]string{
		{"add", "Descope migration fix", "-t", "work", "-t", "dm2", "-t", "backend", "--parent", "CB-20381",
			"-k", "thread:local_a", "-k", "pr:backend-6950"},
		{"note", "thread:local_a", "PR backend#6950: CB-20381: member timeout https://github.com/CBX1/backend/pull/6950"},
		{"start", "thread:local_a"},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps =\n%q\nwant\n%q", steps, want)
	}
}

func TestOldMergedThreadGetsNoWork(t *testing.T) {
	th := Thread{ID: "local_old", Title: "old", PRs: []PR{pr(1, Merged, false)}}
	if steps := Plan([]Thread{th}, nil); len(steps) != 0 {
		t.Fatalf("a thread whose PRs merged long ago got %q", steps)
	}
}

func TestWorkIsDoneOnceNoPRIsOpen(t *testing.T) {
	work := Task{ID: "w1", Title: "fix", Status: "in_progress", Keys: []string{"thread:local_a", "pr:backend-1", "pr:backend-2"}}
	one := Thread{ID: "local_a", PRs: []PR{pr(1, Merged, true), pr(2, Open, true)}}
	if steps := Plan([]Thread{one}, []Task{work}); len(steps) != 0 {
		t.Fatalf("work with a PR still open got %q", steps)
	}

	both := Thread{ID: "local_a", PRs: []PR{pr(1, Merged, true), pr(2, Merged, true)}}
	steps := Plan([]Thread{both}, []Task{work})
	want := [][]string{{"done", "w1", "--outcome", "Merged backend#1 CB-20381: member timeout; backend#2 CB-20381: member timeout"}}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %q, want %q", steps, want)
	}
}

func TestNewPRReopensDoneWork(t *testing.T) {
	work := Task{ID: "w1", Title: "fix", Status: "done", Keys: []string{"thread:local_a", "pr:backend-1"}}
	th := Thread{ID: "local_a", PRs: []PR{pr(1, Merged, true), pr(2, Open, true)}}
	steps := Plan([]Thread{th}, []Task{work})
	want := [][]string{
		{"key", "w1", "pr:backend-2"},
		{"note", "w1", "PR backend#2: CB-20381: member timeout https://github.com/CBX1/backend/pull/2"},
		{"add", "fix", "-k", "thread:local_a"},
		{"start", "w1"},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps =\n%q\nwant\n%q", steps, want)
	}
}

func TestPRHeldByAnotherTaskStaysThere(t *testing.T) {
	first := Task{ID: "w1", Title: "fix", Status: "done", Keys: []string{"thread:local_a", "pr:backend-1"}}
	later := Thread{ID: "local_b", Title: "looked at it", PRs: []PR{pr(1, Merged, true), pr(2, Open, true)}}
	steps := Plan([]Thread{later}, []Task{first})
	want := [][]string{
		{"add", "looked at it", "-t", "work", "-t", "backend", "-k", "thread:local_b", "-k", "pr:backend-2"},
		{"note", "thread:local_b", "PR backend#2: CB-20381: member timeout https://github.com/CBX1/backend/pull/2"},
		{"start", "thread:local_b"},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps =\n%q\nwant\n%q; the held PR stays with w1", steps, want)
	}

	if steps := Plan([]Thread{{ID: "local_c", PRs: []PR{pr(1, Merged, true)}}}, []Task{first}); len(steps) != 0 {
		t.Fatalf("a thread holding only another task's PR got %q", steps)
	}
}

func TestTicket(t *testing.T) {
	if got := Ticket([]PR{{Title: "fix paging"}, {Branch: "ashvin/cb-18304-transcripts"}}); got != "CB-18304" {
		t.Fatalf("Ticket = %q", got)
	}
	if got := Ticket([]PR{{Title: "feat: no ticket", Branch: "feat/abc-12"}}); got != "" {
		t.Fatalf("Ticket = %q, want none", got)
	}
}
