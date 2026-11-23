package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/desktop"
)

// The roles the brief gives a thread.
const (
	roleDiscussion = "discussion"
	roleSub        = "sub-thread"
)

// nowRole is the brief's thread's place. A thread another thread started is
// its sub-thread: it does the work and reports back. Every other thread is a
// discussion thread: Ashvin talks there, and it sees its open sub-threads.
type nowRole struct {
	Kind        string      `json:"kind"`
	Parent      string      `json:"parent,omitempty"`
	ParentTitle string      `json:"parentTitle,omitempty"`
	SubThreads  []subThread `json:"subThreads,omitempty"`
}

// subThread is one of a discussion thread's open sub-threads.
type subThread struct {
	SessionID string        `json:"sessionId"`
	Title     string        `json:"title"`
	State     desktop.State `json:"state"`
	PRs       []string      `json:"prs,omitempty"` // "ox#50 merged"
	SignOff   bool          `json:"signOff,omitempty"`
}

// roleOf places the thread self. threads carry the state ox shows (parked
// included); sessions carry who started each thread and every PR.
func roleOf(self string, threads []nowThread, sessions []desktop.Session) *nowRole {
	if self == "" {
		return nil
	}
	byID := map[string]desktop.Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	if parent := byID[self].Parent(); parent != "" {
		title := byID[parent].Title
		if title == "" {
			title = parent
		}
		return &nowRole{Kind: roleSub, Parent: parent, ParentTitle: title}
	}
	r := &nowRole{Kind: roleDiscussion}
	for _, t := range threads {
		s := byID[t.SessionID]
		if s.Archived || s.Parent() != self {
			continue
		}
		sub := subThread{SessionID: t.SessionID, Title: t.Title, State: t.State, SignOff: signsOff(t.State, s.PRs)}
		for _, p := range s.PRs {
			sub.PRs = append(sub.PRs, prRef(p.Repo, p.Number)+" "+strings.ToLower(p.State))
		}
		r.SubThreads = append(r.SubThreads, sub)
	}
	return r
}

// withOpenSubThreads is every thread that started a sub-thread not yet
// archived. Archiving one would cut its sub-threads off from where they
// report.
func withOpenSubThreads(sessions []desktop.Session) map[string]bool {
	out := map[string]bool{}
	for _, s := range sessions {
		if p := s.Parent(); p != "" && !s.Archived {
			out[p] = true
		}
	}
	return out
}

// signsOff reports a sub-thread its discussion thread can archive: its work
// is done (the thread is done, or every PR it has merged or closed) and it
// isn't running, waiting on Ashvin or parked.
func signsOff(state desktop.State, prs []desktop.PR) bool {
	switch state {
	case desktop.StateDone:
		return true
	case desktop.StateWorking, desktop.StateNeedsYou, desktop.StateParked:
		return false
	}
	if len(prs) == 0 {
		return false
	}
	for _, p := range prs {
		if p.State != "MERGED" && p.State != "CLOSED" {
			return false
		}
	}
	return true
}

// briefRole is the brief's role lines.
func briefRole(r *nowRole) string {
	if r == nil {
		return ""
	}
	if r.Kind == roleSub {
		return fmt.Sprintf("Role: sub-thread of \"%s\". Do the work here and own its PR; report back to that thread by ending your turn.\n", r.ParentTitle)
	}
	var b strings.Builder
	b.WriteString("Role: discussion thread. Talk and decide here; work that changes a repo or ends in a PR goes to a sub-thread you start.\n")
	if len(r.SubThreads) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "Sub-threads (%d):\n", len(r.SubThreads))
	var done []string
	for _, s := range r.SubThreads {
		line := fmt.Sprintf("- %s (%s) · %s", clip(s.Title, 60), s.SessionID, s.State)
		if len(s.PRs) > 0 {
			line += " · " + strings.Join(s.PRs, ", ")
		}
		b.WriteString(line + "\n")
		if s.SignOff {
			done = append(done, fmt.Sprintf("%s (%s)", clip(s.Title, 60), s.SessionID))
		}
	}
	if len(done) > 0 {
		fmt.Fprintf(&b, "Sign off: %s\n", strings.Join(done, "; "))
	}
	return b.String()
}
