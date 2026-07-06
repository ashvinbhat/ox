package review

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/gitutil"
)

// ReviewWorktree is an ephemeral worktree created for a single review session.
type ReviewWorktree struct {
	RepoName string // ox-registered repo name (e.g. "frontend")
	PRNumber int
	Path     string // ~/.ox/worktrees/<repo>/review-<pr>
	Branch   string // local branch tracking the PR head
	BaseRef  string // base branch the PR targets, fully qualified (e.g. origin/main)
}

// CreateReviewWorktree fetches the PR head into a local branch and checks it
// out at ~/.ox/worktrees/<repo>/review-<pr>. The branch name is namespaced as
// `review/pr-<n>` so it doesn't collide with task branches.
//
// If a worktree from a previous review run still exists at the target path
// (typically because the prior run used --keep or failed mid-post), it is
// force-removed first. Branches survive — only the worktree checkout is
// disposable — so prior local work is preserved.
func CreateReviewWorktree(oxHome, repoName string, pr *PRInfo) (*ReviewWorktree, error) {
	repoPath := filepath.Join(oxHome, "repos", repoName)
	if _, err := os.Stat(repoPath); err != nil {
		return nil, fmt.Errorf("repo %q not cloned at %s — register via `ox repo add`", repoName, repoPath)
	}

	worktreePath := filepath.Join(oxHome, "worktrees", repoName, fmt.Sprintf("review-%d", pr.Number))
	branch := fmt.Sprintf("review/pr-%d", pr.Number)

	// If a previous review worktree for this PR is still checked out at the
	// same path, the upcoming `git fetch origin refs/pull/N/head:<branch>`
	// will fail with "refusing to fetch into branch ... checked out at ...".
	// Auto-clean here so re-runs are friction-free. Branch object survives
	// (worktree remove doesn't delete the branch).
	if _, err := os.Stat(worktreePath); err == nil {
		if rmErr := gitutil.RemoveWorktree(repoPath, worktreePath); rmErr != nil {
			// Fall back to forcibly removing the directory if the git command
			// can't (e.g. worktree metadata is corrupt) and prune dangling
			// references so the next worktree-add can succeed.
			_ = os.RemoveAll(worktreePath)
			_ = gitutil.Run(repoPath, "worktree", "prune")
		}
	}

	// Fetch latest base + head. `gh pr checkout` would be simpler but
	// requires the worktree to already exist; we go through git directly to
	// stay consistent with how pickup creates worktrees.
	if err := gitutil.Fetch(repoPath); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", repoName, err)
	}

	// Fetch the PR head explicitly — GitHub exposes it as refs/pull/<n>/head.
	prRef := fmt.Sprintf("refs/pull/%d/head:%s", pr.Number, branch)
	if err := gitutil.Run(repoPath, "fetch", "origin", prRef, "--force"); err != nil {
		return nil, fmt.Errorf("fetch PR head: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return nil, fmt.Errorf("create worktree parent dir: %w", err)
	}

	// `git worktree add` with an existing branch.
	if err := gitutil.Run(repoPath, "worktree", "add", worktreePath, branch); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	baseRef := pr.BaseRef
	if baseRef != "" {
		baseRef = "origin/" + baseRef
	} else {
		baseRef = "origin/main"
	}

	return &ReviewWorktree{
		RepoName: repoName,
		PRNumber: pr.Number,
		Path:     worktreePath,
		Branch:   branch,
		BaseRef:  baseRef,
	}, nil
}

// Remove tears down the review worktree. The local branch is preserved so the
// reviewer can reconstruct the worktree later if needed (`git checkout
// review/pr-<n>`).
func (w *ReviewWorktree) Remove(oxHome string) error {
	repoPath := filepath.Join(oxHome, "repos", w.RepoName)
	return gitutil.RemoveWorktree(repoPath, w.Path)
}
