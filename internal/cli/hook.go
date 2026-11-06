package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/hooks"
	"github.com/ashvinbhat/ox/internal/jobs"
	"github.com/ashvinbhat/ox/internal/livepath"
)

// The background jobs every thread start kicks off. Each is a status file in
// ~/.ox/jobs, so one that keeps failing shows in the brief and on the board.
const (
	jobPRs       = "prs"
	jobWorktrees = "worktrees"
	jobMemory    = "memory"
)

// A run past its limit is killed and counted as a failure. gh calls time out
// at 15s each; the limits leave room for every repo and every PR.
const (
	prsLimit   = 2 * time.Minute
	pruneLimit = 10 * time.Minute
	auditLimit = 10 * time.Minute
)

// briefBudget is how long the hook waits for the brief. Claude Code kills the
// hook at hooks.Timeout; a slow brief is skipped, not a slow session start.
var briefBudget = 10 * time.Second

var hookBrief = func(oxHome string, now time.Time) string {
	return renderBrief(buildNow(oxHome, now))
}

// hookSelf finds the thread a Claude Code session id runs in. A new thread's
// state file may not name its session yet, so it looks a few times.
var hookSelf = func(cliID string) string {
	if cliID == "" {
		return ""
	}
	for try := 0; try < 3; try++ {
		sessions, _ := desktop.List(desktop.DefaultDir(), false)
		if id := desktop.ByCLISession(sessions, cliID); id != "" {
			return id
		}
		time.Sleep(300 * time.Millisecond)
	}
	return ""
}

// jobProblems is one line per job that failed twice in a row or more, saying
// what's stale and why.
func jobProblems(oxHome string, now time.Time) []string {
	var out []string
	for _, j := range []struct{ name, what string }{
		{jobPRs, "PR state"},
		{jobWorktrees, "The worktree prune"},
		{jobMemory, "The memory audit"},
		{jobTickets, "The ticket sync"},
		{jobWork, "Keeping work in yoke"},
	} {
		s := jobs.Load(oxHome, j.name)
		if !s.Failing() {
			continue
		}
		switch {
		case j.name != jobPRs:
			out = append(out, fmt.Sprintf("%s failed %d times in a row: %s", j.what, s.Failures, s.LastError))
		case s.LastSuccess.IsZero():
			out = append(out, fmt.Sprintf("%s was never fetched: %s", j.what, s.LastError))
		default:
			out = append(out, fmt.Sprintf("%s is %s old: %s", j.what, shortAge(now.Sub(s.LastSuccess)), s.LastError))
		}
	}
	return out
}

var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "What ox's Claude Code hooks run",
	Hidden: true,
}

var hookSessionStartCmd = &cobra.Command{
	Use:   "session-start",
	Short: "The SessionStart hook: start the background jobs, print the brief",
	Long: `Claude Code runs this at every thread start. It starts the worktree prune,
the memory audit, the PR refresh and the work refresh in the background, each
only when due, then prints ` + "`ox now --brief`" + ` as the thread's opening
context, with the thread's own key.

A missing config, a panic or a slow brief prints nothing and exits 0, so ox
never blocks a session. With OX_HOOK_CHECK=1 (ox setup's check) no job starts
and the day's reminders stay unsaid.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(sessionStart(os.Stdin, os.Stdout))
	},
}

func sessionStart(stdin io.Reader, stdout io.Writer) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	// Claude Code writes the session's JSON on stdin: its id finds the thread.
	ids := make(chan string, 1)
	go func() {
		var in struct {
			SessionID string `json:"session_id"`
		}
		_ = json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in)
		ids <- in.SessionID
		_, _ = io.Copy(io.Discard, io.LimitReader(stdin, 1<<20))
	}()
	if cfgErr != nil || cfg == nil {
		return 0
	}
	if home, err := livepath.Home(); err == nil {
		_ = os.Setenv("PATH", toolPath(os.Getenv("PATH"), home))
	}
	check := os.Getenv(hooks.CheckEnv) == "1"
	if !check {
		startJobs()
	}

	nowBrief, nowMention = true, !check
	brief := make(chan string, 1)
	go func() {
		defer func() {
			if recover() != nil {
				brief <- ""
			}
		}()
		select {
		case id := <-ids:
			nowSelf = hookSelf(id)
		case <-time.After(time.Second):
		}
		brief <- hookBrief(cfg.Home, time.Now())
	}()
	var text string
	select {
	case text = <-brief:
	case <-time.After(briefBudget):
	}
	if text == "" {
		return 0
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": text,
		},
	})
	return 0
}

// toolPath adds where brew and make install put gh, rg and yoke. A hook can
// get a PATH without them, and every job shells out to one.
func toolPath(path, home string) string {
	dirs := filepath.SplitList(path)
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, "go", "bin")} {
		if !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// startJobs starts each job as its own process group, outliving the hook, so
// a job past its limit can kill its gh children without touching Claude Code.
func startJobs() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	for _, argv := range [][]string{
		{"worktrees", "prune", "--if-due"},
		{"memory", "audit", "--if-due"},
		{"asks", "refresh", "--if-due"},
		{"work", "refresh", "--if-due"},
	} {
		c := exec.Command(self, argv...)
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if c.Start() == nil {
			_ = c.Process.Release()
		}
	}
}

func init() {
	hookCmd.AddCommand(hookSessionStartCmd)
	rootCmd.AddCommand(hookCmd)
}
