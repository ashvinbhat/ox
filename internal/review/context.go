package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PriorContext is the prior-review payload threaded through PrepareWorkspace
// when running a follow-up review. nil = first-time review.
type PriorContext struct {
	PriorHeadSHA   string    // head SHA at the prior review
	PriorFindings  []Finding // findings from the prior review (with Ref labels assigned)
	AddressingDiff string    // git diff <PriorHeadSHA>..<currentHead>, may be empty if SHAs identical
}

// PrepareWorkspace lays out the per-review files inside the worktree under
// .ox/review/: REVIEW.md (context for agents) and findings/ (where each
// agent's JSON output lands).
//
// If prior is non-nil, REVIEW.md gets two extra sections (prior findings +
// addressing diff) that drive each agent's addressing verdicts.
func PrepareWorkspace(w *ReviewWorktree, pr *PRInfo, diff string, extraRules []string, prior *PriorContext) (string, error) {
	dir := filepath.Join(w.Path, ".ox", "review")
	if err := os.MkdirAll(filepath.Join(dir, "findings"), 0o755); err != nil {
		return "", fmt.Errorf("create review dir: %w", err)
	}

	md := buildReviewMD(pr, w, diff, extraRules, prior)
	mdPath := filepath.Join(dir, "REVIEW.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("write REVIEW.md: %w", err)
	}

	return dir, nil
}

func buildReviewMD(pr *PRInfo, w *ReviewWorktree, diff string, extraRules []string, prior *PriorContext) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# Review context — PR #%d\n\n", pr.Number)
	fmt.Fprintf(&sb, "**Title:** %s  \n", pr.Title)
	fmt.Fprintf(&sb, "**Author:** @%s  \n", pr.Author.Login)
	fmt.Fprintf(&sb, "**URL:** %s  \n", pr.URL)
	fmt.Fprintf(&sb, "**Base:** %s  •  **Head:** %s (%s)\n\n", pr.BaseRef, pr.HeadRef, shortSHA(pr.HeadSHA))

	if pr.Body != "" {
		sb.WriteString("## PR description\n\n")
		sb.WriteString(pr.Body)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Files changed\n\n")
	for _, f := range pr.Files {
		fmt.Fprintf(&sb, "- `%s` (+%d / -%d)\n", f.Path, f.Additions, f.Deletions)
	}
	sb.WriteString("\n")

	// Prior review (if follow-up). Placed BEFORE the unified diff so it
	// frames how the agent reads the rest of the context.
	if prior != nil && len(prior.PriorFindings) > 0 {
		fmt.Fprintf(&sb, "## Prior review findings (at SHA %s)\n\n", shortSHA(prior.PriorHeadSHA))
		sb.WriteString("Each finding is labeled with a stable ref (F1, F2, ...) — use these refs in your addressing[] output to grade whether the author addressed them in the diff since last review.\n\n")
		for _, f := range prior.PriorFindings {
			anchor := fmt.Sprintf("%s:%d", f.File, f.Line)
			if f.EndLine > f.Line {
				anchor = fmt.Sprintf("%s:%d-%d", f.File, f.Line, f.EndLine)
			}
			fmt.Fprintf(&sb, "### [%s] %s — %s (%s · %s · agent:%s)\n", f.Ref, anchor, f.Title, f.Severity, f.Category, f.Agent)
			sb.WriteString(f.Body)
			sb.WriteString("\n\n")
		}

		if strings.TrimSpace(prior.AddressingDiff) != "" {
			sb.WriteString("## Diff since last review (addressing diff)\n\n")
			sb.WriteString("This is the diff between the head SHA at the prior review and the current head. Use it to judge addressing status on each prior finding.\n\n")
			sb.WriteString("```diff\n")
			sb.WriteString(prior.AddressingDiff)
			sb.WriteString("\n```\n\n")
		} else {
			sb.WriteString("## Diff since last review (addressing diff)\n\n")
			sb.WriteString("_No commits since the last review — prior findings are still at the same SHA. Mark each as `ignored` unless the prior finding was about something already addressed by the existing diff._\n\n")
		}
	}

	if len(extraRules) > 0 {
		sb.WriteString("## Additional review rules\n\n")
		for _, rule := range extraRules {
			sb.WriteString(rule)
			sb.WriteString("\n\n")
		}
	}

	// CLAUDE.md from the worktree (repo conventions).
	if claude, ok := tryRead(filepath.Join(w.Path, "CLAUDE.md")); ok {
		sb.WriteString("## Repo conventions (CLAUDE.md)\n\n")
		sb.WriteString(claude)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Unified diff (base..head)\n\n")
	sb.WriteString("```diff\n")
	sb.WriteString(diff)
	sb.WriteString("\n```\n")

	return sb.String()
}

func tryRead(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func shortSHA(s string) string {
	if len(s) > 9 {
		return s[:9]
	}
	return s
}
