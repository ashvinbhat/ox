package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/hooks"
	"github.com/ashvinbhat/ox/internal/jobs"
)

func TestSessionStartPrintsTheBriefOrNothing(t *testing.T) {
	t.Setenv(hooks.CheckEnv, "1")
	defer func(c *config.Config, e error, b func(string, time.Time) string, d time.Duration) {
		cfg, cfgErr, hookBrief, briefBudget = c, e, b, d
	}(cfg, cfgErr, hookBrief, briefBudget)
	cfg, cfgErr = &config.Config{Home: t.TempDir()}, nil
	briefBudget = 50 * time.Millisecond

	run := func() string {
		var out bytes.Buffer
		if code := sessionStart(strings.NewReader(`{"hook_event_name":"SessionStart"}`), &out); code != 0 {
			t.Fatalf("exit %d, want 0 always", code)
		}
		return out.String()
	}

	hookBrief = func(string, time.Time) string { return "## ox now\nFull view: `ox now`.\n" }
	var got struct {
		Out struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(run()), &got); err != nil || got.Out.Event != "SessionStart" || !strings.Contains(got.Out.Context, "`ox now`") {
		t.Fatalf("brief output = %+v, %v; want SessionStart context", got, err)
	}

	hookBrief = func(string, time.Time) string { panic("bad state file") }
	if out := run(); out != "" {
		t.Errorf("a panic printed %q, want nothing", out)
	}

	hookBrief = func(string, time.Time) string { time.Sleep(time.Second); return "late" }
	if out := run(); out != "" {
		t.Errorf("a slow brief printed %q, want nothing", out)
	}

	cfgErr = errors.New("no ox.yaml")
	if out := run(); out != "" {
		t.Errorf("a missing config printed %q, want nothing", out)
	}
}

func TestJobProblemsShowUntilASuccess(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	if got := jobProblems(home, now); got != nil {
		t.Fatalf("no jobs run: %v, want none", got)
	}

	jobs.Note(home, jobPRs, errors.New("gh: authentication failed"))
	if got := jobProblems(home, now); got != nil {
		t.Fatalf("one failure: %v, want none", got)
	}
	jobs.Note(home, jobPRs, errors.New("gh: authentication failed"))
	jobs.Note(home, jobWorktrees, errors.New("gh failed on all 3 PR lookups: HTTP 401"))
	jobs.Note(home, jobWorktrees, errors.New("gh failed on all 3 PR lookups: HTTP 401"))
	want := []string{
		"PR state was never fetched: gh: authentication failed",
		"The worktree prune failed 2 times in a row: gh failed on all 3 PR lookups: HTTP 401",
	}
	if got := jobProblems(home, now); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}

	jobs.Note(home, jobWorktrees, nil)
	jobs.Note(home, jobPRs, nil)
	jobs.Note(home, jobPRs, errors.New("gh: timed out"))
	jobs.Note(home, jobPRs, errors.New("gh: timed out"))
	want = []string{"PR state is 2h old: gh: timed out"}
	if got := jobProblems(home, time.Now().Add(2*time.Hour+time.Minute)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestToolPathAddsBrewAndGoBin(t *testing.T) {
	got := toolPath("/usr/bin:/bin:/opt/homebrew/bin", "/Users/me")
	if want := "/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin:/Users/me/go/bin"; got != want {
		t.Errorf("toolPath = %q, want %q", got, want)
	}
}
