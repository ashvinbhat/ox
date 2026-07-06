package review

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PRInfo is the subset of GitHub PR metadata we need to drive a review.
type PRInfo struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	URL       string   `json:"url"`
	BaseRef   string   `json:"baseRefName"`
	HeadRef   string   `json:"headRefName"`
	HeadSHA   string   `json:"headRefOid"`
	Author    ghLogin  `json:"author"`
	HeadRepo  ghRepo   `json:"headRepository"`
	BaseRepo  ghRepo   `json:"headRepositoryOwner"`
	Files     []ghFile `json:"files"`
	OwnerRepo string   // owner/repo of the base — filled in by ResolvePR
}

type ghLogin struct {
	Login string `json:"login"`
}

type ghRepo struct {
	Name  string `json:"name"`
	Login string `json:"login,omitempty"` // for owner objects
}

type ghFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// ResolvePR resolves a PR reference (URL, number, or branch) to PRInfo via gh.
// repoOverride may be "" — gh will infer from the current directory or the URL.
func ResolvePR(ref string, repoOverride string) (*PRInfo, error) {
	args := []string{
		"pr", "view", ref,
		"--json", "number,title,body,url,baseRefName,headRefName,headRefOid,author,headRepository,headRepositoryOwner,files",
	}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}

	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", asGhErr(err))
	}

	var pr PRInfo
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, fmt.Errorf("parse gh pr view output: %w", err)
	}

	// Derive owner/repo. If repoOverride is provided, use it; otherwise infer
	// from the PR URL (https://github.com/<owner>/<repo>/pull/N).
	if repoOverride != "" {
		pr.OwnerRepo = repoOverride
	} else {
		pr.OwnerRepo = ownerRepoFromURL(pr.URL)
	}

	return &pr, nil
}

// Diff returns the PR diff as a unified-diff string via gh.
func Diff(pr *PRInfo) (string, error) {
	args := []string{"pr", "diff", fmt.Sprintf("%d", pr.Number)}
	if pr.OwnerRepo != "" {
		args = append(args, "--repo", pr.OwnerRepo)
	}
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		return "", fmt.Errorf("gh pr diff: %w", asGhErr(err))
	}
	return string(out), nil
}

// DiffBetween returns a git unified diff between two SHAs within the given
// repo directory. Used to compute the addressing diff (prior head → current
// head) for follow-up reviews. If either SHA is unreachable locally,
// returns ("", err).
func DiffBetween(repoDir, fromSHA, toSHA string) (string, error) {
	cmd := exec.Command("git", "diff", "--no-color", fmt.Sprintf("%s..%s", fromSHA, toSHA))
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff %s..%s: %w", fromSHA, toSHA, asGhErr(err))
	}
	return string(out), nil
}

func ownerRepoFromURL(url string) string {
	// https://github.com/<owner>/<repo>/pull/<n>
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
	return parts[0] + "/" + parts[1]
}

// asGhErr unwraps an ExitError to surface stderr, which is where gh prints
// useful diagnostics (not-authed, not-found, etc.). Common auth failures
// are translated into an actionable hint.
func asGhErr(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		msg := strings.TrimSpace(string(ee.Stderr))
		lower := strings.ToLower(msg)
		// Heuristics for unauthenticated gh — be generous with matchers since
		// the exact text varies by gh version.
		switch {
		case strings.Contains(lower, "not authenticated") ||
			strings.Contains(lower, "no authentication token") ||
			strings.Contains(lower, "could not find any authentication"):
			return fmt.Errorf("%s\n\nRun `gh auth login` to authenticate, then retry", msg)
		case strings.Contains(lower, "404") && strings.Contains(lower, "not found"):
			return fmt.Errorf("%s\n\nIs the PR number correct? If the repo is private, ensure your gh token has access", msg)
		}
		return fmt.Errorf("%s", msg)
	}
	return err
}

// CheckGHAuth runs `gh auth status` and returns nil if authenticated.
// Used to fail fast with a useful message before doing any other work.
func CheckGHAuth() error {
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh is not authenticated — run `gh auth login` to set it up, then retry")
	}
	return nil
}
