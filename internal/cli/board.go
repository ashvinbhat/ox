package cli

import (
	"sort"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/stops"
	"github.com/ashvinbhat/ox/internal/work"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

// boardView is what the ox board draws: the stops waiting on Ashvin, the
// work in flight, and what landed today.
type boardView struct {
	Needs    []boardNeed   `json:"needs"`
	Snoozed  int           `json:"snoozed,omitempty"`
	InFlight []boardFlight `json:"inFlight"`
	Landed   []string      `json:"landed,omitempty"` // PRs merged today, oldest first
}

// boardNeed is one thread stopped on Ashvin.
type boardNeed struct {
	ID     string    `json:"id"`             // the ask's, for a snooze
	Kind   string    `json:"kind,omitempty"` // the stop, when the thread named one
	Title  string    `json:"title"`
	What   string    `json:"what"`
	Since  time.Time `json:"since"`
	Thread string    `json:"thread"`
}

// boardFlight is one thread with work moving, under its ticket if it has one.
type boardFlight struct {
	Ticket string        `json:"ticket,omitempty"`
	Title  string        `json:"title"`
	State  desktop.State `json:"state"`
	PRs    []string      `json:"prs,omitempty"`
	Since  time.Time     `json:"since"`
	Thread string        `json:"thread"`
}

// flightRank orders work in flight: moving first, parked last.
var flightRank = map[desktop.State]int{desktop.StateWorking: 0, desktop.StateStuck: 1, desktop.StateInReview: 2, desktop.StateParked: 3}

func buildBoard(q asks.Queue, threads []nowThread, sessions []desktop.Session, tasks []yokecli.Task,
	log []stops.Entry, merged map[string]time.Time, now time.Time) boardView {
	b := boardView{Needs: []boardNeed{}, InFlight: []boardFlight{}}
	byID := map[string]desktop.Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	waiting := map[string]bool{}
	for _, a := range q.Waiting {
		if n, ok := needOf(a, byID, log); ok {
			b.Needs = append(b.Needs, n)
			waiting[a.Thread] = true
		}
	}
	for _, a := range q.Snoozed {
		if _, ok := needOf(a, byID, log); ok {
			b.Snoozed++
		}
	}

	tickets := threadTickets(tasks)
	for _, t := range threads {
		if _, moving := flightRank[t.State]; !moving || waiting[t.SessionID] {
			continue
		}
		b.InFlight = append(b.InFlight, boardFlight{Ticket: tickets[t.SessionID], Title: t.Title, State: t.State,
			PRs: t.PRs, Since: t.LastActive, Thread: t.SessionID})
	}
	sortFlights(b.InFlight)

	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	for ref, at := range merged {
		if !at.Before(today) {
			b.Landed = append(b.Landed, ref)
		}
	}
	sort.Slice(b.Landed, func(i, j int) bool { return merged[b.Landed[i]].Before(merged[b.Landed[j]]) })
	return b
}

// needOf turns a thread's question or a PR ready to merge into a board line.
// A question takes the kind and words of the stop the thread asked, when
// the ask hook let one through since Ashvin last wrote there.
func needOf(a asks.Ask, threads map[string]desktop.Session, log []stops.Entry) (boardNeed, bool) {
	if a.Thread == "" {
		return boardNeed{}, false
	}
	n := boardNeed{ID: a.ID, Title: a.Where, What: a.What, Since: a.Since, Thread: a.Thread}
	switch a.Kind {
	case asks.KindQuestion:
		if e, ok := lastStop(threads[a.Thread], log); ok {
			n.Kind = e.Kind
			first, _, _ := strings.Cut(e.Question, "\n")
			_, words, _ := strings.Cut(first, ":")
			n.What = strings.TrimSpace(words)
		}
		if a.Settled != "" {
			n.What += " (" + a.Settled + "; still needed?)"
		}
	case asks.KindMerge:
		ref, _, _ := strings.Cut(strings.TrimPrefix(a.What, "Merge "), ":")
		n.Kind, n.What = "Merge", ref+" is ready to merge"
	default:
		return boardNeed{}, false
	}
	return n, true
}

// lastStop is the newest stop a thread asked after Ashvin's latest message
// there. A thread runs under several Claude Code session ids over its life.
func lastStop(s desktop.Session, log []stops.Entry) (stops.Entry, bool) {
	ids := map[string]bool{}
	for _, id := range append([]string{s.CLISessionID, s.LastCLIID}, s.PriorCLIIDs...) {
		if id != "" {
			ids[id] = true
		}
	}
	since := time.UnixMilli(int64(s.LastUserMs))
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		if e.Passed && ids[e.Session] && e.At.After(since) {
			return e, true
		}
	}
	return stops.Entry{}, false
}

// threadTickets names each thread's ticket: the parent of the work its
// thread key holds.
func threadTickets(tasks []yokecli.Task) map[string]string {
	byID := map[string]yokecli.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	out := map[string]string{}
	for _, t := range tasks {
		if t.ParentID == nil {
			continue
		}
		parent, ok := byID[*t.ParentID]
		if !ok || !parent.HasTag(work.TagTicket) {
			continue
		}
		for _, k := range t.Keys {
			if id, ok := strings.CutPrefix(strings.ToLower(k), "thread:"); ok {
				out[id] = strings.TrimSpace(ticketKey(parent) + " " + parent.Title)
			}
		}
	}
	return out
}

// sortFlights keeps a ticket's threads together. Tickets go by their most
// moving thread, then the latest move; a thread with no ticket is its own group.
func sortFlights(fs []boardFlight) {
	group := func(f boardFlight) string {
		if f.Ticket != "" {
			return "t:" + f.Ticket
		}
		return "s:" + f.Thread
	}
	best := map[string]boardFlight{}
	for _, f := range fs {
		g := group(f)
		if b, ok := best[g]; !ok || before(f, b) {
			best[g] = f
		}
	}
	sort.SliceStable(fs, func(i, j int) bool {
		gi, gj := group(fs[i]), group(fs[j])
		if gi != gj {
			return before(best[gi], best[gj])
		}
		return before(fs[i], fs[j])
	})
}

func before(a, b boardFlight) bool {
	if flightRank[a.State] != flightRank[b.State] {
		return flightRank[a.State] < flightRank[b.State]
	}
	return a.Since.After(b.Since)
}
