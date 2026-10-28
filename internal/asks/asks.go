// Package asks gathers what waits on Ashvin into one ranked queue: thread
// questions, review asks, his PRs ready to merge, memory suggestions and a
// due recap. PR problems a thread can fix on its own (conflicts, failing CI,
// requested changes) are listed apart, so only real decisions wait on him.
package asks

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind is where an ask comes from.
type Kind string

const (
	KindQuestion Kind = "question"
	KindReview   Kind = "review"
	KindMerge    Kind = "merge"
	KindMemory   Kind = "memory"
	KindRecap    Kind = "recap"
	KindFix      Kind = "fix"
)

// rank orders the queue: a blocked thread first, then a teammate waiting on
// a review, then his own PRs, then housekeeping.
var rank = map[Kind]int{KindQuestion: 0, KindReview: 1, KindMerge: 2, KindMemory: 3, KindRecap: 4}

// StaleAfter is when a thread's question gets "still needed?".
const StaleAfter = 3 * 24 * time.Hour

// Ask is one thing waiting on Ashvin, or one fix waiting on a thread.
type Ask struct {
	ID      string    `json:"id"`
	Kind    Kind      `json:"kind"`
	What    string    `json:"what"`
	Where   string    `json:"where,omitempty"`  // the thread it belongs to
	Link    string    `json:"link,omitempty"`   // PR URL or thread id
	Thread  string    `json:"thread,omitempty"` // id of the thread it belongs to
	Track   string    `json:"track,omitempty"`
	Since   time.Time `json:"since"`
	Minutes int       `json:"minutes,omitempty"` // rough size
	Stale   bool      `json:"stale,omitempty"`
}

// Queue is the ranked list for Ashvin and the fixes left to threads.
type Queue struct {
	Waiting []Ask     `json:"waiting"`
	Fixes   []Ask     `json:"fixes"`
	Dropped int       `json:"dropped,omitempty"`
	PRsAt   time.Time `json:"prsAt,omitzero"` // when PR state was fetched
}

// Thread is a desktop thread as the queue sees it.
type Thread struct {
	ID       string
	Title    string
	Ask      string // set when the thread waits on Ashvin
	Since    time.Time
	PRs      []string // short refs, as PR.Ref gives them
	Archived bool
	Track    string
}

// Review is a review ask from the ledger. From is who asked.
type Review struct {
	Ref, URL, From, Why, Track string
	ReReview                   bool
	Since                      time.Time
}

// Inputs is everything the queue is built from.
type Inputs struct {
	Threads           []Thread
	Reviews           []Review
	PRs               []PR
	PRsAt             time.Time
	MemorySuggestions int
	MemoryAt          time.Time
	RecapDue          bool
	DecisionsWeek     int
}

// Build ranks the asks, leaving out any Ashvin dropped that hasn't changed
// since.
func Build(in Inputs, dropped map[string]time.Time, now time.Time) Queue {
	var all []Ask
	for _, t := range in.Threads {
		if t.Ask == "" || t.Archived {
			continue
		}
		all = append(all, Ask{ID: "question:" + t.ID, Kind: KindQuestion, What: t.Ask, Where: t.Title, Link: t.ID,
			Thread: t.ID, Track: t.Track, Since: t.Since, Minutes: 2, Stale: now.Sub(t.Since) > StaleAfter})
	}
	for _, r := range in.Reviews {
		what := fmt.Sprintf("Review %s for %s", r.Ref, firstName(r.From))
		if r.ReReview {
			what = fmt.Sprintf("Re-review %s (%s)", r.Ref, r.Why)
		}
		all = append(all, Ask{ID: "review:" + r.Ref, Kind: KindReview, What: what, Link: r.URL, Track: r.Track, Since: r.Since, Minutes: 30})
	}
	owners := prThreads(in.Threads)
	for _, p := range in.PRs {
		if a, ok := prAsk(p, owners[p.Ref()]); ok {
			all = append(all, a)
		}
	}
	if n := in.MemorySuggestions; n > 0 {
		all = append(all, Ask{ID: "memory", Kind: KindMemory, What: fmt.Sprintf("Memory: %s (/memory-check)", plural(n, "suggestion")),
			Since: in.MemoryAt, Minutes: 5 * ((n + 4) / 5)})
	}
	if in.RecapDue {
		all = append(all, Ask{ID: "recap", Kind: KindRecap, What: fmt.Sprintf("Recap: %s this week (/recap)", plural(in.DecisionsWeek, "decision")),
			Since: now, Minutes: 10})
	}

	q := Queue{PRsAt: in.PRsAt}
	for _, a := range all {
		if at, ok := dropped[a.ID]; ok && !a.Since.After(at) {
			q.Dropped++
			continue
		}
		if a.Kind == KindFix {
			q.Fixes = append(q.Fixes, a)
		} else {
			q.Waiting = append(q.Waiting, a)
		}
	}
	sort.SliceStable(q.Waiting, func(i, j int) bool {
		a, b := q.Waiting[i], q.Waiting[j]
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.Since.Before(b.Since)
	})
	sort.SliceStable(q.Fixes, func(i, j int) bool { return q.Fixes[i].Since.Before(q.Fixes[j].Since) })
	return q
}

// prAsk turns a PR's state into a fix for its thread (conflict, failing CI,
// changes requested) or a merge for Ashvin, or nothing while it's in review.
func prAsk(p PR, owner Thread) (Ask, bool) {
	var problems []string
	if p.Mergeable == "CONFLICTING" {
		problems = append(problems, "merge conflict")
	}
	if p.Checks == ChecksFailing {
		problems = append(problems, "CI failing")
	}
	if p.Review == "CHANGES_REQUESTED" {
		problems = append(problems, "changes requested")
	}
	ref := p.Ref()
	if len(problems) > 0 {
		what := ref + ": " + strings.Join(problems, ", ")
		if p.Draft {
			what += " (draft)"
		}
		return Ask{ID: "fix:" + ref, Kind: KindFix, What: what, Where: owner.Title, Link: p.URL, Thread: owner.ID, Track: owner.Track,
			Since: p.UpdatedAt, Minutes: 15}, true
	}
	approved := p.Review == "APPROVED" || p.Mine && p.Review == ""
	green := p.Checks == ChecksPassing || p.Checks == ChecksNone
	if !p.Draft && p.Mergeable == "MERGEABLE" && green && approved {
		return Ask{ID: "merge:" + ref, Kind: KindMerge, What: "Merge " + ref + ": " + p.Title, Where: owner.Title, Link: p.URL,
			Thread: owner.ID, Track: owner.Track, Since: p.UpdatedAt, Minutes: 1}, true
	}
	return Ask{}, false
}

// prThreads maps a PR's short ref to the open thread that owns it.
func prThreads(threads []Thread) map[string]Thread {
	out := map[string]Thread{}
	for _, t := range threads {
		if t.Archived {
			continue
		}
		for _, pr := range t.PRs {
			if _, ok := out[pr]; !ok {
				out[pr] = t
			}
		}
	}
	return out
}

func firstName(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
