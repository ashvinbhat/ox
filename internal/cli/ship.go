package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokecli"
	"github.com/spf13/cobra"
)

var (
	shipRepo   string
	shipDraft  bool
	shipDryRun bool
)

var shipCmd = &cobra.Command{
	Use:   "ship [task-id]",
	Short: "Push branches and create PRs",
	Long: `Pushes task branches to remote and creates pull requests.

This command:
1. Pushes all worktree branches to origin
2. Creates a PR for each repo using gh CLI
3. Links PRs in the output

Examples:
  ox ship              # Ship all repos in current workspace
  ox ship 9            # Ship specific task
  ox ship --repo backend   # Ship only backend repo
  ox ship --draft      # Create draft PRs`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShip,
}

func runShip(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Find workspace
	var ws *workspace.TaskWorkspace
	var err error

	ref := ""
	if len(args) > 0 {
		ref = args[0]
	}
	ws, err = resolveWorkspace(cfg.Home, ref)
	if err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	// Get task info for PR title/body and for linking PRs back to the
	// yoke task as notes (one task can have multiple PRs, one per repo
	// or revision — each becomes a separate note).
	var taskTitle, taskID string
	var taskSeq int
	if t, err := yokecli.Get(fmt.Sprintf("%d", ws.TaskSeq)); err == nil {
		taskTitle = t.Title
		taskSeq = t.Seq
		taskID = t.ID
	}
	if taskTitle == "" {
		taskTitle = ws.Slug
		taskSeq = ws.TaskSeq
	}

	// Find repos to ship
	reposToShip := findReposInWorkspace(ws.Path)
	if shipRepo != "" {
		// Filter to specific repo
		found := false
		for _, r := range reposToShip {
			if r == shipRepo {
				reposToShip = []string{shipRepo}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repo %q not in workspace", shipRepo)
		}
	}

	if len(reposToShip) == 0 {
		return fmt.Errorf("no repos found in workspace")
	}

	fmt.Printf("Shipping task #%d: %s\n\n", taskSeq, taskTitle)

	var prs []string

	for _, repoName := range reposToShip {
		worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", ws.TaskSeq))

		// Check if there are commits to push
		branch, err := gitutil.CurrentBranch(worktreePath)
		if err != nil {
			fmt.Printf("Warning: could not get branch for %s: %v\n", repoName, err)
			continue
		}

		fmt.Printf("=== %s (%s) ===\n", repoName, branch)

		if shipDryRun {
			fmt.Printf("  Would push %s to origin\n", branch)
			fmt.Printf("  Would create PR\n\n")
			continue
		}

		// Push branch
		fmt.Printf("  Pushing %s...\n", branch)
		if err := gitutil.Push(worktreePath, branch); err != nil {
			fmt.Printf("  Warning: push failed: %v\n", err)
			continue
		}

		// Create PR using gh CLI
		prURL, err := createPR(worktreePath, repoName, taskSeq, taskTitle, shipDraft)
		if err != nil {
			fmt.Printf("  Warning: PR creation failed: %v\n", err)
			continue
		}

		fmt.Printf("  PR: %s\n\n", prURL)
		prs = append(prs, fmt.Sprintf("%s: %s", repoName, prURL))

		// Link the PR back to the yoke task as a note. Multiple PRs per task
		// are supported (one note per PR, repo-tagged so they're scannable).
		if linkErr := linkPRToTask(taskID, repoName, prURL); linkErr != nil {
			fmt.Printf("  Warning: failed to link PR to yoke task: %v\n", linkErr)
		} else {
			fmt.Printf("  Linked PR to task #%d in yoke\n\n", taskSeq)
		}
	}

	if len(prs) > 0 && !shipDryRun {
		fmt.Println("Created PRs:")
		for _, pr := range prs {
			fmt.Printf("  %s\n", pr)
		}
	}

	return nil
}

// linkPRToTask attaches a "PR (<repo>): <url>" note to the yoke task so
// future runs of `ox status`, the dashboard, etc. can surface every PR
// associated with the task. One task can carry multiple PRs (one per repo,
// or revisions / fork-rebuilds).
func linkPRToTask(taskID, repoName, prURL string) error {
	if taskID == "" || prURL == "" {
		return nil
	}

	// De-dupe: if a note already records this exact URL, skip.
	notes, err := yokecli.Notes(taskID)
	if err == nil {
		for _, n := range notes {
			if strings.Contains(n.Content, prURL) {
				return nil
			}
		}
	}
	note := fmt.Sprintf("PR (%s): %s", repoName, prURL)
	return yokecli.AddNote(taskID, note)
}

// findReposInWorkspace returns repo names that have symlinks in the workspace.
// Only directory targets count — doc symlinks (CLAUDE.md, YOKE.md) are files.
func findReposInWorkspace(workspacePath string) []string {
	var repos []string
	entries, err := os.ReadDir(workspacePath)
	if err != nil {
		return repos
	}

	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Stat(filepath.Join(workspacePath, e.Name()))
		if err != nil || !target.IsDir() {
			continue
		}
		repos = append(repos, e.Name())
	}
	return repos
}

// createPR creates a pull request using gh CLI.
//
// Title and body deliberately avoid referencing the local task tracker
// (no "Task #N", no "Shipped via ox ship" footer, no yoke) so nothing
// leaks about the workspace tooling into the PR that reviewers on GitHub
// see. The title is the task title verbatim; the body is a plain summary
// the author is expected to expand on. Task-linking still happens via
// `ox link-pr` on the yoke side — that's an internal side-channel.
func createPR(worktreePath, repoName string, taskSeq int, taskTitle string, draft bool) (string, error) {
	_ = taskSeq // no longer surfaced in the PR itself
	title := taskTitle
	body := fmt.Sprintf("## Summary\n%s\n", taskTitle)

	args := []string{"pr", "create", "--title", title, "--body", body}
	if draft {
		args = append(args, "--draft")
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = worktreePath
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if PR already exists
		if strings.Contains(string(output), "already exists") {
			// Get existing PR URL
			return getExistingPR(worktreePath)
		}
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}

	return extractPRURL(string(output))
}

// extractPRURL picks the PR URL out of gh's output. CombinedOutput interleaves
// stderr warnings (e.g. "Warning: 1 uncommitted change") with the URL, so
// taking the whole output verbatim would poison downstream note content.
func extractPRURL(output string) (string, error) {
	url := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			url = line
		}
	}
	if url == "" {
		return "", fmt.Errorf("no PR URL in gh output: %s", strings.TrimSpace(output))
	}
	return url, nil
}

// getExistingPR returns the URL of an existing PR for the current branch.
func getExistingPR(worktreePath string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "url", "-q", ".url")
	cmd.Dir = worktreePath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func init() {
	shipCmd.Flags().StringVarP(&shipRepo, "repo", "r", "", "Ship only this repo")
	shipCmd.Flags().BoolVar(&shipDraft, "draft", false, "Create draft PRs")
	shipCmd.Flags().BoolVar(&shipDryRun, "dry-run", false, "Show what would be done")

	rootCmd.AddCommand(shipCmd)
}
