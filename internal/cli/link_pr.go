package cli

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ashvinbhat/ox/internal/yokecli"
	"github.com/spf13/cobra"
)

var linkPRRepo string

var linkPRCmd = &cobra.Command{
	Use:   "link-pr <task-id> <pr-url-or-number>",
	Short: "Link a PR to a yoke task",
	Long: `Attaches a PR URL to a yoke task as a note.

Tasks can carry multiple PRs (one per repo, or revisions). Each invocation
adds a separate "PR (<repo>): <url>" note on the task. Duplicate URLs are
skipped silently.

ox ship calls this automatically after creating a PR. Use this command
when:
- A PR was created outside ox (gh pr create, web UI, etc.)
- A follow-up PR was raised on top of the original
- You re-pushed a branch and want to record the resulting PR

Examples:
  ox link-pr 9 https://github.com/CBX1/backend/pull/4321
  ox link-pr 9 4321 --repo backend
  ox link-pr 9 https://github.com/CBX1/frontend/pull/5862`,
	Args: cobra.ExactArgs(2),
	RunE: runLinkPR,
}

func runLinkPR(cmd *cobra.Command, args []string) error {
	taskRef := args[0]
	prRef := args[1]

	t, err := yokecli.Get(taskRef)
	if err != nil {
		return fmt.Errorf("task %s not found: %w", taskRef, err)
	}

	// Resolve PR URL — if it's a bare number, ask gh to give us the canonical URL.
	prURL, repoName, err := resolvePRRef(prRef, linkPRRepo)
	if err != nil {
		return fmt.Errorf("resolve PR: %w", err)
	}

	// Dedupe against existing notes.
	notes, _ := yokecli.Notes(t.ID)
	for _, n := range notes {
		if strings.Contains(n.Content, prURL) {
			fmt.Printf("PR already linked to task #%d (note already references %s)\n", t.Seq, prURL)
			return nil
		}
	}

	label := repoName
	if label == "" {
		label = "pr"
	}
	note := fmt.Sprintf("PR (%s): %s", label, prURL)
	if err := yokecli.AddNote(t.ID, note); err != nil {
		return fmt.Errorf("add note to task: %w", err)
	}
	fmt.Printf("✓ Linked %s to task #%d (%s)\n", prURL, t.Seq, t.Title)
	return nil
}

// resolvePRRef takes either a full GitHub PR URL or a bare PR number plus a
// repo hint and returns the canonical URL + repo name. For bare numbers
// without a repo hint we ask gh to infer from the current directory.
func resolvePRRef(prRef, repoHint string) (url, repo string, err error) {
	prRef = strings.TrimSpace(prRef)
	if strings.HasPrefix(prRef, "http://") || strings.HasPrefix(prRef, "https://") {
		// Extract repo from URL: https://github.com/<owner>/<repo>/pull/<n>
		repo = repoFromPRURL(prRef)
		return prRef, repo, nil
	}

	// Bare number — fetch URL via gh.
	args := []string{"pr", "view", prRef, "--json", "url,headRepository", "-q", "."}
	if repoHint != "" {
		args = append(args, "--repo", repoHint)
	}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return "", "", fmt.Errorf("gh pr view %s: %w", prRef, err)
	}
	var resp struct {
		URL            string `json:"url"`
		HeadRepository struct {
			Name string `json:"name"`
		} `json:"headRepository"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", "", fmt.Errorf("parse gh pr view: %w", err)
	}
	return resp.URL, resp.HeadRepository.Name, nil
}

// repoFromPRURL returns the repo name (last path segment of owner/repo)
// from a github PR URL, or "" if it can't be parsed.
func repoFromPRURL(url string) string {
	const marker = "github.com/"
	idx := strings.Index(url, marker)
	if idx < 0 {
		return ""
	}
	rest := url[idx+len(marker):]
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func init() {
	linkPRCmd.Flags().StringVar(&linkPRRepo, "repo", "", "Repo hint (owner/repo) for resolving bare PR numbers via gh")
	rootCmd.AddCommand(linkPRCmd)
}
