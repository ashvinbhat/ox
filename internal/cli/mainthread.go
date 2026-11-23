package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/desktop"
)

// The roles the brief gives a thread.
const (
	roleMain = "main"
	roleSub  = "sub-thread"
	roleWork = "work"
)

// nowRole is the brief's thread's place: Main orchestrates, its sub-threads
// do the work, any other thread is a work thread. Main also sees its open
// sub-threads.
type nowRole struct {
	Kind       string      `json:"kind"`
	SubThreads []subThread `json:"subThreads,omitempty"`
}

// subThread is one of Main's open sub-threads.
type subThread struct {
	SessionID string        `json:"sessionId"`
	Title     string        `json:"title"`
	State     desktop.State `json:"state"`
	PRs       []string      `json:"prs,omitempty"` // "ox#50 merged"
	SignOff   bool          `json:"signOff,omitempty"`
}

var mainCmd = &cobra.Command{
	Use:   "main [<desktop id>]",
	Short: "Record which desktop thread is Main, or print it",
	Long: `Main is the one thread Ashvin chats in. It orchestrates: every new piece of
work goes to a sub-thread it starts. With an id (local_…), ox records it in
~/.ox/main; without one, it prints the id it has.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		if len(args) == 0 {
			id := mainThread(cfg.Home)
			if id == "" {
				return errors.New("no Main yet: ox main <local_…> records one")
			}
			fmt.Println(id)
			return nil
		}
		s, err := desktop.Get(desktop.DefaultDir(), args[0])
		if err != nil {
			return err
		}
		if err := setMainThread(cfg.Home, s.ID); err != nil {
			return err
		}
		fmt.Printf("Main: %s (%s)\n", s.ID, s.Title)
		return nil
	},
}

func mainPath(oxHome string) string {
	return filepath.Join(oxHome, "main")
}

// mainThread is Main's id, "" when none is recorded.
func mainThread(oxHome string) string {
	b, err := os.ReadFile(mainPath(oxHome))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func setMainThread(oxHome, id string) error {
	if err := os.MkdirAll(oxHome, 0o755); err != nil {
		return err
	}
	return os.WriteFile(mainPath(oxHome), []byte(id+"\n"), 0o644)
}

// roleOf places the thread self. threads carry the state ox shows (parked
// included); sessions carry who started each thread and every PR.
func roleOf(self, mainID string, threads []nowThread, sessions []desktop.Session) *nowRole {
	if self == "" {
		return nil
	}
	byID := map[string]desktop.Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	if self != mainID {
		if mainID != "" && byID[self].Parent() == mainID {
			return &nowRole{Kind: roleSub}
		}
		return &nowRole{Kind: roleWork}
	}
	r := &nowRole{Kind: roleMain}
	for _, t := range threads {
		s := byID[t.SessionID]
		if s.Archived || s.Parent() != mainID {
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

// signsOff reports a sub-thread Main can archive: its work is done (the
// thread is done, or every PR it has merged or closed) and it isn't running,
// waiting on Ashvin or parked.
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
	switch r.Kind {
	case roleSub:
		return "Role: sub-thread of Main. Do the work here, own its PR, report back by ending your turn.\n"
	case roleWork:
		return "Role: work thread.\n"
	}
	var b strings.Builder
	b.WriteString("Role: Main. Orchestrate only: start a sub-thread for any new work.\n")
	if len(r.SubThreads) == 0 {
		b.WriteString("Sub-threads: none open\n")
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

func init() {
	rootCmd.AddCommand(mainCmd)
}
