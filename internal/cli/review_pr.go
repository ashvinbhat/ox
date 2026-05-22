package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/review"
	"github.com/spf13/cobra"
)

var (
	reviewPRRepo        string
	reviewPRKeep        bool
	reviewPRNonInter    bool
	reviewPRRulesFile   string
	reviewPRConcurrency int
	reviewPRDryRun      bool
)

var reviewPRCmd = &cobra.Command{
	Use:           "pr <pr-ref>",
	Short:         "Review a GitHub pull request",
	SilenceUsage:  true, // runtime errors should not dump cobra's --help
	SilenceErrors: false,
	Long: `Reviews a GitHub pull request in an ephemeral worktree.

Creates a review worktree at ~/.ox/worktrees/<repo>/review-<pr>, builds a
review context (PR metadata + full diff + repo conventions), and runs a
reviewer agent to produce findings anchored to file:line. Findings are
rendered to the terminal.

The PR reference can be a URL, a PR number, or a branch name (per gh pr view).

Examples:
  ox review pr 5467
  ox review pr 5467 --repo frontend
  ox review pr https://github.com/CBX1/frontend/pull/5467
  ox review pr 5467 --rules ~/.ox/review-rules.md
  ox review pr 5467 --keep            # keep the worktree after review`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewPR,
}

func runReviewPR(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	prRef := args[0]

	// Fail fast if gh isn't authenticated — every downstream step needs it.
	if err := review.CheckGHAuth(); err != nil {
		return err
	}

	repoFlag := reviewPRRepo
	ghRepoArg := ""
	if repoFlag != "" {
		// If the user gave an ox-registered repo name, translate to owner/repo for gh.
		if rc, ok := cfg.Repos[repoFlag]; ok {
			ghRepoArg = ownerRepoFromGitURL(rc.URL)
		} else if strings.Contains(repoFlag, "/") {
			// Looks like owner/repo already.
			ghRepoArg = repoFlag
		} else {
			return fmt.Errorf("repo %q not registered in ox (and not in owner/repo form)", repoFlag)
		}
	}

	fmt.Printf("🐂 Resolving PR %s...\n", prRef)
	pr, err := review.ResolvePR(prRef, ghRepoArg)
	if err != nil {
		return fmt.Errorf("resolve PR: %w", err)
	}

	// Resolve the ox-registered repo name from owner/repo so we know where the
	// worktree should live.
	repoName := repoFlag
	if repoName == "" || strings.Contains(repoName, "/") {
		repoName = repoNameByOwnerRepo(cfg, pr.OwnerRepo)
		if repoName == "" {
			return fmt.Errorf("PR is in %s but no matching repo is registered in ox (use `ox repo add` first, or pass --repo with the ox name)", pr.OwnerRepo)
		}
	}

	fmt.Printf("   #%d %s\n", pr.Number, pr.Title)
	headLabel := pr.HeadRef
	baseOwner := ""
	if i := strings.Index(pr.OwnerRepo, "/"); i > 0 {
		baseOwner = pr.OwnerRepo[:i]
	}
	if pr.BaseRepo.Login != "" && pr.BaseRepo.Login != baseOwner {
		// PR head is on a fork — show "owner:branch" so reviewer sees the cross-repo origin.
		headLabel = pr.BaseRepo.Login + ":" + pr.HeadRef
	}
	fmt.Printf("   @%s · %s → %s\n", pr.Author.Login, headLabel, pr.BaseRef)
	fmt.Printf("   %d files changed\n", len(pr.Files))

	fmt.Println("🐂 Creating review worktree...")
	wt, err := review.CreateReviewWorktree(cfg.Home, repoName, pr)
	if err != nil {
		return fmt.Errorf("create review worktree: %w", err)
	}
	// Worktree cleanup is deferred but gated on `cleanupWorktree`. We flip
	// this to false if anything fails after the worktree exists, so the
	// reviewer can inspect REVIEW.md / findings / logs without rerunning.
	cleanupWorktree := !reviewPRKeep
	defer func() {
		if !cleanupWorktree {
			fmt.Printf("\nWorktree kept for debugging: %s\n", wt.Path)
			fmt.Printf("Findings:  %s/.ox/review/findings/\n", wt.Path)
			fmt.Printf("Agent logs: %s/.ox/review/logs/\n", wt.Path)
			return
		}
		fmt.Println("🐂 Cleaning up review worktree...")
		if rerr := wt.Remove(cfg.Home); rerr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove review worktree: %v\n", rerr)
		}
	}()

	fmt.Println("🐂 Fetching diff...")
	diff, err := review.Diff(pr)
	if err != nil {
		return fmt.Errorf("fetch diff: %w", err)
	}

	// Load prior review state, if any, and build follow-up context.
	state, err := review.LoadState(cfg.Home, pr.OwnerRepo, pr.Number)
	if err != nil {
		return fmt.Errorf("load review state: %w", err)
	}

	var priorCtx *review.PriorContext
	priorRefsByAgent := map[string][]string{}
	priorByRef := map[string]review.Finding{}
	if last := state.LastReview(); last != nil && last.HeadSHA != pr.HeadSHA {
		fmt.Printf("🐂 Follow-up review: prior review at SHA %s, current head %s\n", short(last.HeadSHA), short(pr.HeadSHA))
		addressingDiff, derr := review.DiffBetween(wt.Path, last.HeadSHA, pr.HeadSHA)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not compute addressing diff (prior SHA may be unreachable): %v\n", derr)
			fmt.Fprintln(os.Stderr, "         falling back to first-time review semantics.")
		} else {
			priorCtx = &review.PriorContext{
				PriorHeadSHA:   last.HeadSHA,
				PriorFindings:  last.Findings,
				AddressingDiff: addressingDiff,
			}
			for _, f := range last.Findings {
				priorByRef[f.Ref] = f
				priorRefsByAgent[f.Agent] = append(priorRefsByAgent[f.Agent], f.Ref)
			}
		}
	} else if last := state.LastReview(); last != nil {
		fmt.Println("🐂 No commits since the last review — treating as a re-run, not a follow-up.")
	}

	// Discover rules in precedence order: global → per-repo → --rules path.
	var extraPaths []string
	if reviewPRRulesFile != "" {
		extraPaths = append(extraPaths, reviewPRRulesFile)
	}
	ruleSources, err := review.DiscoverRules(cfg.Home, wt.Path, extraPaths)
	if err != nil {
		return fmt.Errorf("discover review rules: %w", err)
	}
	if len(ruleSources) > 0 {
		fmt.Printf("🐂 Loaded %d review-rule source(s): ", len(ruleSources))
		origins := make([]string, len(ruleSources))
		for i, rs := range ruleSources {
			origins[i] = rs.Origin
		}
		fmt.Println(strings.Join(origins, ", "))
	}

	if _, err := review.PrepareWorkspace(wt, pr, diff, review.FormatForContext(ruleSources), priorCtx); err != nil {
		return fmt.Errorf("prepare review workspace: %w", err)
	}

	fmt.Println("🐂 Running reviewers...")
	findings, addressing, results, err := review.RunAll(wt, review.AllAgents, priorRefsByAgent, reviewPRConcurrency, os.Stdout)
	if err != nil {
		return fmt.Errorf("run reviewers: %w", err)
	}

	// Surface a one-line summary if any agent failed (its log is preserved for inspection).
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s agent did not complete cleanly — see %s\n", r.Spec.Name, r.LogPath)
		}
	}

	// Render output. Interactive flow renders the summary inside
	// RunInteractive, so don't double-print here. Non-interactive mode
	// gets a full all-bodies dump piped through $PAGER when stdout is a
	// TTY and the suite has 5+ findings.
	if reviewPRNonInter {
		w, closePager := review.MaybePager(os.Stdout, len(findings))
		review.Render(w, findings)
		if len(addressing) > 0 {
			renderAddressingSummary(w, addressing, priorByRef)
		}
		_ = closePager()
	} else if len(addressing) > 0 {
		// Addressing block goes here so it shows before the (interactive)
		// findings summary, framing the follow-up context first.
		renderAddressingSummary(os.Stdout, addressing, priorByRef)
	}

	if reviewPRKeep {
		fmt.Printf("\nWorktree kept: %s\n", wt.Path)
		fmt.Printf("Findings JSON: %s/.ox/review/findings/\n", wt.Path)
	}

	// Always record this run in the state file, even for self-PRs and
	// non-interactive runs, so a future follow-up has prior context to
	// reason about. Ref labels (F1, F2, ...) get assigned by SaveState.
	record := review.ReviewRecord{
		ReviewedAt: time.Now().UTC(),
		HeadSHA:    pr.HeadSHA,
		Findings:   findings,
	}

	// Posting flow. Skip entirely for non-interactive mode or self-PRs.
	if reviewPRNonInter || (len(findings) == 0 && len(addressing) == 0) {
		return saveStateOrWarn(cfg.Home, state, pr, record)
	}

	if isSelf, _ := isSelfPR(pr); isSelf {
		fmt.Println("\nThis is your own PR — skipping GitHub posting. Findings recorded locally for next follow-up.")
		return saveStateOrWarn(cfg.Home, state, pr, record)
	}

	sel, err := review.RunInteractive(findings, addressing, priorByRef, wt, os.Stdin, os.Stdout)
	if err != nil {
		return fmt.Errorf("interactive selection: %w", err)
	}
	// "Nothing to post" = no findings AND no addressing replies AND no
	// bare-review event. A bare-review event (approve / request-changes /
	// comment with no inline findings) is a valid post path.
	if sel == nil || (len(sel.Findings) == 0 && len(sel.Addressing) == 0 && sel.Event == "") {
		fmt.Println("\nNothing posted.")
		return saveStateOrWarn(cfg.Home, state, pr, record)
	}

	// User confirmed the preview. Post the new review (if any new findings)
	// and any addressing replies (each on its own prior-comment thread).
	if reviewPRDryRun {
		fmt.Println("\n--dry-run set — not posting to GitHub. Selected items would have been:")
		fmt.Printf("  %d inline finding(s), %d addressing reply/replies, event=%s\n", len(sel.Findings), len(sel.Addressing), sel.Event)
		return saveStateOrWarn(cfg.Home, state, pr, record)
	}
	// Post the review when we have either inline findings OR a bare-review
	// event with no inline findings. The Post() call handles both shapes.
	if len(sel.Findings) > 0 || sel.Event != "" {
		fmt.Printf("\n🐂 Posting review to %s ...\n", pr.URL)
		result, err := review.Post(pr, sel)
		if err != nil {
			cleanupWorktree = false
			// Save state anyway — the local review still happened even if posting failed.
			_ = saveStateOrWarn(cfg.Home, state, pr, record)
			return fmt.Errorf("post review: %w", err)
		}
		fmt.Printf("✓ Review posted: %s\n", result.HTMLURL)
		record.Posted = true
		record.ReviewID = result.ReviewID
		record.ReviewURL = result.HTMLURL
		// Stamp comment IDs onto the saved findings so follow-up replies work.
		for i := range record.Findings {
			for j := range sel.Findings {
				if sameFinding(record.Findings[i], sel.Findings[j]) {
					record.Findings[i].CommentID = result.CommentID[sel.Findings[j].Ref]
					break
				}
			}
		}
	}
	for _, a := range sel.Addressing {
		prior, ok := sel.PriorByRef[a.Ref]
		if !ok || prior.CommentID == 0 {
			continue
		}
		if err := review.PostReply(pr.OwnerRepo, pr.Number, prior.CommentID, review.AddressingReply(a)); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to post reply on %s (comment %d): %v\n", a.Ref, prior.CommentID, err)
			continue
		}
		fmt.Printf("✓ Replied to %s\n", a.Ref)
	}

	return saveStateOrWarn(cfg.Home, state, pr, record)
}

// short returns the first 9 chars of a SHA, or the whole string if shorter.
func short(s string) string {
	if len(s) > 9 {
		return s[:9]
	}
	return s
}

// sameFinding heuristically matches a record finding to a posted (sel)
// finding so we can stamp the CommentID on the record. Equal file+line+title
// is enough since dedupe already collapsed exact dupes.
func sameFinding(a, b review.Finding) bool {
	return a.File == b.File && a.Line == b.Line && a.Title == b.Title
}

// saveStateOrWarn appends `record` to `state` (creating it if needed) and
// writes back to disk. A save failure is logged but does not fail the
// overall command — the review still happened, the user can re-run.
func saveStateOrWarn(oxHome string, state *review.State, pr *review.PRInfo, record review.ReviewRecord) error {
	if state == nil {
		state = &review.State{
			PR:        pr.Number,
			OwnerRepo: pr.OwnerRepo,
		}
	}
	state.AppendRecord(record)
	if err := review.SaveState(oxHome, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save review state: %v\n", err)
	}
	return nil
}

// renderAddressingSummary prints a quick read of the per-prior-finding
// verdicts. Used after the main Render() call when this is a follow-up.
func renderAddressingSummary(w io.Writer, addressing []review.Addressing, priorByRef map[string]review.Finding) {
	fmt.Fprintln(w, "\n── Addressing of prior findings ──")
	byRef := map[string][]review.Addressing{}
	order := []string{}
	for _, a := range addressing {
		if _, seen := byRef[a.Ref]; !seen {
			order = append(order, a.Ref)
		}
		byRef[a.Ref] = append(byRef[a.Ref], a)
	}
	for _, ref := range order {
		prior, hasPrior := priorByRef[ref]
		anchor := ""
		if hasPrior {
			anchor = fmt.Sprintf(" %s:%d — %s", prior.File, prior.Line, prior.Title)
		}
		fmt.Fprintf(w, "%s%s\n", ref, anchor)
		for _, a := range byRef[ref] {
			icon := map[review.AddressingStatus]string{
				review.AddressingAddressed: "✓",
				review.AddressingPartial:   "⚠",
				review.AddressingIgnored:   "✗",
			}[a.Status]
			fmt.Fprintf(w, "  %s %s (agent: %s) — %s\n", icon, a.Status, a.Agent, a.Note)
		}
	}
	fmt.Fprintln(w)
}

// isSelfPR returns true when the authenticated gh user authored the PR.
// Best-effort: any error fetching the gh user is treated as "not self" so a
// reviewer without gh authed can still proceed with the posting prompt
// (the actual gh api POST will surface a more useful error).
func isSelfPR(pr *review.PRInfo) (bool, error) {
	me, err := review.CurrentGHUser()
	if err != nil {
		return false, err
	}
	return strings.EqualFold(me, pr.Author.Login), nil
}

// ownerRepoFromGitURL extracts "owner/repo" from a git URL like
// git@github.com:owner/repo.git or https://github.com/owner/repo.git
func ownerRepoFromGitURL(url string) string {
	url = strings.TrimSuffix(url, ".git")
	if i := strings.Index(url, "github.com"); i >= 0 {
		rest := url[i+len("github.com"):]
		rest = strings.TrimLeft(rest, ":/")
		return rest
	}
	return ""
}

// repoNameByOwnerRepo finds the ox-registered repo name matching the given
// "owner/repo" string by comparing against each repo's clone URL.
func repoNameByOwnerRepo(cfg *config.Config, ownerRepo string) string {
	for name, rc := range cfg.Repos {
		if ownerRepoFromGitURL(rc.URL) == ownerRepo {
			return name
		}
	}
	return ""
}

func init() {
	reviewPRCmd.Flags().StringVar(&reviewPRRepo, "repo", "", "Ox-registered repo name (or owner/repo) — required if PR ref is just a number and gh can't infer from cwd")
	reviewPRCmd.Flags().BoolVar(&reviewPRKeep, "keep", false, "Keep the review worktree after the review completes")
	reviewPRCmd.Flags().BoolVar(&reviewPRNonInter, "no-interactive", false, "Skip the interactive posting prompt — print findings only")
	reviewPRCmd.Flags().StringVar(&reviewPRRulesFile, "rules", "", "Path to a markdown file with additional review rules to inject as context")
	reviewPRCmd.Flags().IntVar(&reviewPRConcurrency, "concurrency", 0, "Max reviewer agents to run at once (0 = all in parallel, default)")
	reviewPRCmd.Flags().BoolVar(&reviewPRDryRun, "dry-run", false, "Walk through the interactive flow but don't actually POST to GitHub")
	reviewCmd.AddCommand(reviewPRCmd)
}
