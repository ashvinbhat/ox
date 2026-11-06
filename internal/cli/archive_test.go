package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/park"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

func TestArchivableNeedsEveryRule(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	quiet := float64(now.Add(-30 * time.Hour).UnixMilli())
	done := &desktop.Summary{Status: "done"}
	thread := func(id string) desktop.Session {
		return desktop.Session{ID: id, Title: id, CreatedMs: 1, LastActiveMs: quiet, Summary: done}
	}
	ok := thread("local_ok")
	recent := thread("local_recent")
	recent.LastActiveMs = float64(now.Add(-time.Hour).UnixMilli())
	asking := thread("local_asking")
	asking.Summary = &desktop.Summary{Status: "done", NeedsAction: "Which fix first?"}
	parkedT := thread("local_parked")
	withPR := thread("local_pr")
	withPR.PRs = []desktop.PR{{Repo: "CBX1/backend", Number: 1, State: "OPEN"}}
	busy := thread("local_work")
	dirtyT := thread("local_dirty")
	dirtyT.Cwd = "/r/backend/.claude/worktrees/x"
	shared := thread("local_shared")
	shared.Cwd = "/r/backend"

	got := archivable(
		[]desktop.Session{ok, recent, asking, parkedT, withPR, busy, dirtyT, shared},
		map[string]park.Record{"local_parked": {At: now.Add(-20 * time.Hour)}},
		[]yokecli.Task{{Status: "in_progress", Keys: []string{"thread:local_work"}}, {Status: "done", Keys: []string{"thread:local_ok"}}},
		[]asks.PR{{Repo: "CBX1/backend", Number: 1}},
		now,
		func(dir string) bool { return true },
	)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, ",") != "local_ok,local_shared" {
		t.Fatalf("archivable = %v; want only the thread meeting every rule, and one in a shared checkout", ids)
	}
}

func TestArchivedLine(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.Local)
	if got := archivedLine(home, now); got != "" {
		t.Fatalf("with nothing archived: %q", got)
	}
	if err := saveArchived(home, now.AddDate(0, 0, -1), []archiveCandidate{{ID: "a", Title: "Slack message review"}}); err != nil {
		t.Fatal(err)
	}
	if got := archivedLine(home, now); got != "Archived yesterday: 1 thread (Slack message review)" {
		t.Fatalf("archivedLine = %q", got)
	}
}
