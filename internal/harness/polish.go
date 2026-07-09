package harness

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/mission"
)

const polishTimeout = 6 * time.Minute

// PreShipPolish removes comment debris (WHAT-comments, signature-restating
// javadocs) that the branch added, as a blocking gate before push. The edit
// is made by a headless job, but a deterministic guard only lets pure
// comment/blank-line changes through — anything touching code discards the
// whole pass. Best-effort: every failure path ships the branch untouched.
func PreShipPolish(cfg *config.Config, m *mission.Mission, repoName string, binding *mission.RepoBinding) string {
	wt := binding.IntegrationWorktree
	if out, err := gitOut(wt, "status", "--porcelain"); err != nil || strings.TrimSpace(out) != "" {
		return "polish skipped: worktree not clean"
	}

	base := "main"
	if r, ok := cfg.Repos[repoName]; ok && r.BaseBranch != "" {
		base = r.BaseBranch
	}
	prompt := strings.ReplaceAll(embedded("polish.md"), "BASE_BRANCH", base)

	j, err := job.Start(cfg, m, job.StartInput{
		ID:       fmt.Sprintf("polish-%s-%d", repoName, time.Now().Unix()),
		Prompt:   prompt,
		Model:    "sonnet",
		CWD:      wt,
		MaxTurns: 30,
	})
	if err != nil {
		return "polish skipped: " + err.Error()
	}

	deadline := time.Now().Add(polishTimeout)
	for {
		if done, _ := job.Harvest(cfg, m, j); done {
			break
		}
		if time.Now().After(deadline) {
			if j.PID > 0 {
				syscall.Kill(-j.PID, syscall.SIGKILL)
			}
			gitOut(wt, "checkout", "--", ".")
			return "polish skipped: timeout"
		}
		time.Sleep(5 * time.Second)
	}
	if j.Status != job.StatusDone {
		gitOut(wt, "checkout", "--", ".")
		return "polish skipped: job failed"
	}

	status, err := gitOut(wt, "status", "--porcelain")
	if err != nil {
		return "polish skipped: " + err.Error()
	}
	if strings.TrimSpace(status) == "" {
		return "polish: clean"
	}
	for _, line := range strings.Split(strings.TrimSpace(status), "\n") {
		if strings.HasPrefix(line, "??") {
			gitOut(wt, "checkout", "--", ".")
			return "polish discarded: job created files"
		}
	}

	diff, err := gitOut(wt, "diff")
	if err != nil || !commentOnlyDiff(diff) {
		gitOut(wt, "checkout", "--", ".")
		return "polish discarded: edits touched code"
	}

	removed := 0
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
			removed++
		}
	}
	if _, err := gitOut(wt, "commit", "-am", "chore: prune redundant comments"); err != nil {
		gitOut(wt, "checkout", "--", ".")
		return "polish skipped: commit failed"
	}
	m.AppendEvent("polish_done", "system", map[string]any{
		"repo": repoName, "removed_lines": removed,
	})
	return fmt.Sprintf("polish: removed %d comment line(s), committed", removed)
}

// commentOnlyDiff reports whether every changed line is a whole-line comment,
// part of a block comment/docstring, or blank — the only edits the polish
// pass is allowed to make.
func commentOnlyDiff(diff string) bool {
	changed := false
	for _, line := range strings.Split(diff, "\n") {
		if len(line) == 0 || (line[0] != '+' && line[0] != '-') {
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		changed = true
		if !commentish(strings.TrimSpace(line[1:])) {
			return false
		}
	}
	return changed
}

func commentish(s string) bool {
	if s == "" {
		return true
	}
	for _, p := range []string{"//", "/*", "*", "#", "--", "{/*", "<!--", `"""`, "'''"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
