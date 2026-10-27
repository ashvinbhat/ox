// Package worktrees removes git worktrees whose pull request has merged.
//
// Worktrees an open desktop thread runs in are left alone: the app removes
// those itself when it archives the thread. Everything else (worktrees
// threads made with `git worktree add`, mission worktrees, manual ones) is
// removed once its PR merges, but only when nothing on disk would be lost.
package worktrees

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/gitutil"
)

// Worktree is one linked worktree of a repo (never the main checkout).
type Worktree struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// Outcome is what Prune did, or would do, with a worktree whose PR merged.
type Outcome struct {
	Worktree
	PR      int    `json:"pr"`
	Removed bool   `json:"removed"`
	Reason  string `json:"reason,omitempty"` // why it was kept
}

// PR is the merged pull request for a branch.
type PR struct {
	Number  int
	HeadSHA string
}

// List returns the linked worktrees of the repo at repoDir, skipping the main
// checkout and detached worktrees.
func List(repoDir string) ([]Worktree, error) {
	out, err := gitutil.RunOutput(repoDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parsePorcelain(repoDir, out), nil
}

func parsePorcelain(repoDir, out string) []Worktree {
	var (
		list  []Worktree
		path  string
		first = true
	)
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			if !first {
				list = append(list, Worktree{Repo: repoDir, Path: path, Branch: strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")})
			}
		case line == "":
			if path != "" {
				first = false
			}
			path = ""
		}
	}
	return list
}

// Decide says whether a worktree whose PR merged can go. It is kept when a
// thread works in it, it has uncommitted or untracked files, or its HEAD has
// commits the merged PR never saw.
func Decide(wt Worktree, pr PR, inUse map[string]bool, dirty bool, head string) (remove bool, reason string) {
	switch {
	case inUse[filepath.Clean(wt.Path)]:
		return false, "an open thread works in it"
	case dirty:
		return false, "uncommitted changes"
	case head == "" || head != pr.HeadSHA:
		return false, "local commits not in the merged PR"
	}
	return true, ""
}

// Prune walks the worktrees of every repo and removes those whose PR merged
// and that Decide clears. With dryRun nothing changes on disk.
func Prune(repos []string, inUse map[string]bool, dryRun bool) ([]Outcome, error) {
	var outcomes []Outcome
	for _, repo := range repos {
		if !dryRun {
			// Drops registrations of worktree dirs deleted by hand.
			_ = gitutil.Run(repo, "worktree", "prune")
		}
		list, err := List(repo)
		if err != nil {
			continue
		}
		for _, wt := range list {
			pr, ok := mergedPR(repo, wt.Branch)
			if !ok {
				continue
			}
			status, statusErr := gitutil.RunOutput(wt.Path, "status", "--porcelain")
			dirty := statusErr != nil || strings.TrimSpace(status) != ""
			head, _ := gitutil.RunOutput(wt.Path, "rev-parse", "HEAD")

			o := Outcome{Worktree: wt, PR: pr.Number}
			remove, reason := Decide(wt, pr, inUse, dirty, strings.TrimSpace(head))
			if !remove {
				o.Reason = reason
				outcomes = append(outcomes, o)
				continue
			}
			if !dryRun {
				if err := gitutil.Run(repo, "worktree", "remove", wt.Path); err != nil {
					o.Reason = "git refused: " + firstLine(err.Error())
					outcomes = append(outcomes, o)
					continue
				}
				_ = gitutil.Run(repo, "branch", "-D", wt.Branch)
			}
			o.Removed = true
			outcomes = append(outcomes, o)
		}
	}
	return outcomes, nil
}

// mergedPR asks GitHub for the merged PR whose head is branch, in the repo
// behind dir's origin remote.
func mergedPR(dir, branch string) (PR, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--head", branch, "--state", "merged",
		"--limit", "1", "--json", "number,headRefOid")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return PR{}, false
	}
	var found []struct {
		Number     int    `json:"number"`
		HeadRefOid string `json:"headRefOid"`
	}
	if json.Unmarshal(out, &found) != nil || len(found) == 0 {
		return PR{}, false
	}
	return PR{Number: found[0].Number, HeadSHA: found[0].HeadRefOid}, true
}

// MainCheckout returns the main worktree of the repo containing dir.
func MainCheckout(dir string) (string, error) {
	out, err := gitutil.RunOutput(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(out)
	if filepath.Base(common) != ".git" {
		return "", fmt.Errorf("%s: not a standard checkout", dir)
	}
	return filepath.Dir(common), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
