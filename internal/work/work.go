// Package work keeps a yoke task for every desktop thread with a PR. A
// thread's task is found by the thread's key (thread:<local id>) and holds
// a key per PR (pr:<repo>-<n>). A PR key another task holds stays there: the
// app drops old PRs from a thread's list, so the same PR can later show up
// on a thread that only looked at it. The task is started while one of its
// PRs is open and done once they merged. Plan only decides; the caller runs
// the commands it returns.
package work

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// PR states.
const (
	Open   = "open"
	Merged = "merged"
	Closed = "closed"
)

// Tags ox puts on the tasks it keeps.
const (
	TagWork    = "work"
	TagTicket  = "ticket"
	TagFinding = "finding"
)

// PR is one of a thread's pull requests.
type PR struct {
	Repo   string // owner/name
	Number int
	Branch string
	Title  string
	URL    string
	State  string // Open, Merged or Closed
	Recent bool   // open, or merged in the last two weeks
}

// Ref is the short form ox shows: "backend#6952".
func (p PR) Ref() string { return fmt.Sprintf("%s#%d", repoName(p.Repo), p.Number) }

// Thread is a desktop thread and its PRs.
type Thread struct {
	ID    string // local_…
	Title string
	Track string
	PRs   []PR
}

// Task is what Plan reads of a yoke task.
type Task struct {
	ID     string
	Title  string
	Status string
	Keys   []string
}

// ThreadKey finds the work a thread did.
func ThreadKey(id string) string { return "thread:" + id }

// PRKey finds the work that holds a PR.
func PRKey(repo string, n int) string {
	return fmt.Sprintf("pr:%s-%d", strings.ToLower(repoName(repo)), n)
}

func repoName(repo string) string {
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		return repo[i+1:]
	}
	return repo
}

var ticketID = regexp.MustCompile(`(?i)\bCB-\d+\b`)

// Ticket is the first CB id a PR's title or branch names, or "".
func Ticket(prs []PR) string {
	for _, p := range prs {
		for _, s := range []string{p.Title, p.Branch} {
			if id := ticketID.FindString(s); id != "" {
				return strings.ToUpper(id)
			}
		}
	}
	return ""
}

type entry struct {
	ref    string // what the commands name the task by
	title  string
	status string
	keys   map[string]bool // lower case
}

func (e *entry) complete() bool { return e.status == "done" || e.status == "dropped" }

// Plan returns the yoke commands, as argument lists, that bring tasks in
// line with threads. A thread with no task gets one only while a PR of its,
// held by no other task, is open or merged recently.
func Plan(threads []Thread, tasks []Task) (steps [][]string) {
	byKey := map[string]*entry{}
	for _, t := range tasks {
		e := &entry{ref: t.ID, title: t.Title, status: t.Status, keys: map[string]bool{}}
		for _, k := range t.Keys {
			e.keys[strings.ToLower(k)] = true
			byKey[strings.ToLower(k)] = e
		}
	}
	prs := map[string]PR{}
	for _, th := range threads {
		for _, p := range th.PRs {
			k := PRKey(p.Repo, p.Number)
			if old, ok := prs[k]; !ok || rank(p.State) > rank(old.State) {
				prs[k] = p
			}
		}
	}

	var touched []*entry
	seen := map[*entry]bool{}
	for _, th := range threads {
		if len(th.PRs) == 0 {
			continue
		}
		key := strings.ToLower(ThreadKey(th.ID))
		e := byKey[key]
		keys := []string{key}
		var free []PR
		for _, p := range th.PRs {
			k := PRKey(p.Repo, p.Number)
			if holder := byKey[k]; holder == nil || holder == e {
				keys = append(keys, k)
				free = append(free, p)
			}
		}
		var added []string
		if e == nil {
			if !anyRecent(free) {
				continue
			}
			e = &entry{ref: keys[0], title: th.Title, status: "pending", keys: map[string]bool{}}
			steps = append(steps, create(th, free, keys, byKey))
			added = keys
		} else {
			for _, k := range keys {
				if !e.keys[k] {
					added = append(added, k)
				}
			}
			if len(added) > 0 {
				steps = append(steps, append([]string{"key", e.ref}, added...))
			}
		}
		for _, k := range keys {
			e.keys[k] = true
			byKey[k] = e
		}
		for _, k := range added {
			if p, ok := prs[k]; ok {
				steps = append(steps, []string{"note", e.ref, prNote(p)})
			}
		}
		if !seen[e] {
			seen[e] = true
			touched = append(touched, e)
		}
	}

	for _, e := range touched {
		var open, merged []PR
		for k := range e.keys {
			switch p := prs[k]; p.State {
			case Open:
				open = append(open, p)
			case Merged:
				merged = append(merged, p)
			}
		}
		switch {
		case len(open) > 0 && e.complete():
			steps = append(steps, []string{"add", e.title, "-k", reopenKey(e)}, []string{"start", e.ref})
		case len(open) > 0 && e.status == "pending":
			steps = append(steps, []string{"start", e.ref})
		case len(open) == 0 && len(merged) > 0 && !e.complete():
			steps = append(steps, []string{"done", e.ref, "--outcome", outcome(merged)})
		}
	}
	return steps
}

// reopenKey is the key `yoke add` reopens e by: a thread's when it has one.
// A done task's id isn't a key, so add needs one of these.
func reopenKey(e *entry) string {
	var keys []string
	for k := range e.keys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasPrefix(k, "thread:") {
			return k
		}
	}
	return keys[0]
}

func create(th Thread, prs []PR, keys []string, byKey map[string]*entry) []string {
	args := []string{"add", th.Title, "-t", TagWork}
	if th.Track != "" {
		args = append(args, "-t", th.Track)
	}
	repos := map[string]bool{strings.ToLower(th.Track): true}
	for _, p := range prs {
		if r := strings.ToLower(repoName(p.Repo)); !repos[r] {
			repos[r] = true
			args = append(args, "-t", r)
		}
	}
	if id := Ticket(prs); id != "" && byKey[strings.ToLower(id)] != nil {
		args = append(args, "--parent", id)
	}
	for _, k := range keys {
		args = append(args, "-k", k)
	}
	return args
}

func anyRecent(prs []PR) bool {
	for _, p := range prs {
		if p.Recent {
			return true
		}
	}
	return false
}

// rank orders states so a PR two threads show takes the furthest one.
func rank(state string) int {
	switch state {
	case Merged:
		return 3
	case Closed:
		return 2
	case Open:
		return 1
	}
	return 0
}

func prNote(p PR) string {
	parts := []string{"PR " + p.Ref()}
	if p.Title != "" {
		parts[0] += ":"
		parts = append(parts, p.Title)
	}
	if p.URL != "" {
		parts = append(parts, p.URL)
	}
	return strings.Join(parts, " ")
}

func outcome(merged []PR) string {
	sort.Slice(merged, func(i, j int) bool { return merged[i].Ref() < merged[j].Ref() })
	var parts []string
	for _, p := range merged {
		s := p.Ref()
		if p.Title != "" {
			s += " " + p.Title
		}
		parts = append(parts, s)
	}
	return "Merged " + strings.Join(parts, "; ")
}
