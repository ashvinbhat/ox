package gitutil

import (
	"fmt"
	"os/exec"
	"strings"
)

// Clone clones a git repository.
func Clone(url, dest string) error {
	return Run(".", "clone", url, dest)
}

// CreateWorktree creates a git worktree.
func CreateWorktree(repoPath, worktreePath, branch string) error {
	// First create the branch if it doesn't exist
	if err := Run(repoPath, "branch", branch); err != nil {
		// Branch might already exist, that's ok
	}

	return Run(repoPath, "worktree", "add", worktreePath, branch)
}

// CreateWorktreeFromRef creates a git worktree from a specific ref.
func CreateWorktreeFromRef(repoPath, worktreePath, branch, ref string) error {
	return Run(repoPath, "worktree", "add", "-b", branch, worktreePath, ref)
}

// RemoveWorktree removes a git worktree.
func RemoveWorktree(repoPath, worktreePath string) error {
	return Run(repoPath, "worktree", "remove", worktreePath, "--force")
}

// ListWorktrees lists all worktrees for a repo.
func ListWorktrees(repoPath string) ([]string, error) {
	output, err := RunOutput(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var worktrees []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			worktrees = append(worktrees, strings.TrimPrefix(line, "worktree "))
		}
	}
	return worktrees, nil
}

// CurrentBranch returns the current branch name.
func CurrentBranch(repoPath string) (string, error) {
	output, err := RunOutput(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// Fetch fetches from remote.
func Fetch(repoPath string) error {
	return Run(repoPath, "fetch", "--prune")
}

// Push pushes a branch to remote.
func Push(repoPath, branch string) error {
	return Run(repoPath, "push", "-u", "origin", branch)
}

// Run executes a git command.
func Run(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return nil
}

// RunOutput executes a git command and returns stdout.
func RunOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, exitErr.Stderr)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}
