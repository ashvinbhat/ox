package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/tracks"
	"github.com/ashvinbhat/ox/internal/work"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

// findingGroup is one board line: the findings waiting for Ashvin's go under
// one ticket, or under one track when they have no ticket.
type findingGroup struct {
	ID      string    `json:"id"` // findings:CB-18304, findings:track:ox
	Title   string    `json:"title"`
	Count   int       `json:"count"`
	Since   time.Time `json:"since"` // the newest finding's
	Dropped bool      `json:"dropped,omitempty"`
}

// maxHops bounds the walk from a finding to its ticket: finding, its work,
// the work's ticket, with room for a finding found from another.
const maxHops = 6

// findingGroups groups the pending findings by the ticket their parent or
// found-from chain reaches, else by their track. all is every task, so the
// chain can pass through done work.
func findingGroups(all []yokecli.Task, ts []tracks.Track) []findingGroup {
	byID := map[string]yokecli.Task{}
	for _, t := range all {
		byID[t.ID] = t
	}
	groups := map[string]*findingGroup{}
	for _, t := range all {
		if !t.HasTag(work.TagFinding) || t.Status != yokecli.StatusPending {
			continue
		}
		id, title := groupOf(t, byID, ts)
		g := groups[id]
		if g == nil {
			g = &findingGroup{ID: id, Title: title}
			groups[id] = g
		}
		g.Count++
		if t.CreatedAt.After(g.Since) {
			g.Since = t.CreatedAt
		}
	}
	out := make([]findingGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.After(out[j].Since) })
	return out
}

func groupOf(t yokecli.Task, byID map[string]yokecli.Task, ts []tracks.Track) (id, title string) {
	cur := t
	for hop := 0; hop < maxHops; hop++ {
		if cur.HasTag(work.TagTicket) {
			key := ticketKey(cur)
			return "findings:" + key, strings.TrimSpace(key + " " + cur.Title)
		}
		next := cur.ParentID
		if next == nil {
			next = cur.FoundFromID
		}
		if next == nil {
			break
		}
		up, ok := byID[*next]
		if !ok {
			break
		}
		cur = up
	}
	for _, tr := range ts {
		if t.HasTag(tr.Label()) {
			return "findings:track:" + tr.Label(), tr.Name + " (no ticket)"
		}
	}
	return "findings:none", "No ticket"
}

func ticketKey(t yokecli.Task) string {
	for _, k := range t.Keys {
		if strings.HasPrefix(strings.ToUpper(k), "CB-") {
			return strings.ToUpper(k)
		}
	}
	return fmt.Sprintf("#%d", t.Seq)
}

// markDroppedFindings hides a group cleared from the board until a finding
// newer than the clear arrives under it.
func markDroppedFindings(groups []findingGroup, dropped map[string]time.Time) {
	for i, g := range groups {
		if at, ok := dropped[g.ID]; ok && !g.Since.After(at) {
			groups[i].Dropped = true
		}
	}
}
