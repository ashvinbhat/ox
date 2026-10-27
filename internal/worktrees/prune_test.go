package worktrees

import "testing"

func TestParsePorcelainSkipsMainCheckoutAndDetached(t *testing.T) {
	out := `worktree /repos/backend
HEAD 1111
branch refs/heads/main

worktree /repos/backend/.claude/worktrees/fix-a
HEAD 2222
branch refs/heads/claude/fix-a

worktree /repos/backend/.claude/worktrees/old
HEAD 3333
detached

worktree /ox/worktrees/backend/chore-x
HEAD 4444
branch refs/heads/chore/x
`
	got := parsePorcelain("/repos/backend", out)
	if len(got) != 2 {
		t.Fatalf("got %d worktrees, want 2: %+v", len(got), got)
	}
	if got[0].Branch != "claude/fix-a" || got[1].Path != "/ox/worktrees/backend/chore-x" || got[1].Branch != "chore/x" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecide(t *testing.T) {
	wt := Worktree{Path: "/wt/a", Branch: "b"}
	pr := PR{Number: 6781, HeadSHA: "abc"}
	cases := []struct {
		name   string
		inUse  map[string]bool
		dirty  bool
		head   string
		remove bool
	}{
		{"clean and matches the PR", nil, false, "abc", true},
		{"thread works in it", map[string]bool{"/wt/a": true}, false, "abc", false},
		{"uncommitted changes", nil, true, "abc", false},
		{"local commits after merge", nil, false, "def", false},
		{"head unknown", nil, false, "", false},
	}
	for _, c := range cases {
		remove, reason := Decide(wt, pr, c.inUse, c.dirty, c.head)
		if remove != c.remove {
			t.Errorf("%s: remove = %v (%s), want %v", c.name, remove, reason, c.remove)
		}
		if !remove && reason == "" {
			t.Errorf("%s: kept without a reason", c.name)
		}
	}
}
