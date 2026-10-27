package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/ghreview"
)

var (
	reviewJSON    bool
	reviewAll     bool
	reviewOffline bool
	reviewFile    string
	reviewConfirm bool
	askFrom       string
	askWhere      string
	askLink       string
	askNote       string
	askAt         string
)

var reviewsCmd = &cobra.Command{
	Use:   "reviews",
	Short: "Your review queue: asked, re-review due, waiting on author",
	Long: `Lists the PRs in your review ledger (~/.ox/reviews/): ones someone asked
you to review, ones with new commits or a new ask since your last round, and
ones waiting on the author. Only PRs you were asked about or reviewed appear;
team review requests are not pulled in.

Checks each open PR on GitHub first; --offline uses what the ledger knows.`,
	Args: cobra.NoArgs,
	RunE: runReviews,
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review a PR against the whole codebase (used by /review-pr)",
}

var reviewAskCmd = &cobra.Command{
	Use:   "ask <pr-url>",
	Short: "Record that someone asked you to review a PR",
	Args:  cobra.ExactArgs(1),
	RunE:  runReviewAsk,
}

var reviewStartCmd = &cobra.Command{
	Use:   "start <pr-url>",
	Short: "Check out the PR head and gather the diff, interdiff and open findings",
	Long: `Checks out the PR head in ~/.ox/worktrees/<repo>/review-<n> (a full
checkout, so the review can read the whole codebase), writes the PR diff, and
on a re-review the diff since your last reviewed head plus every finding not
yet addressed. Files land in ~/.ox/reviews/work/<owner>-<repo>-<n>/.`,
	Args: cobra.ExactArgs(1),
	RunE: runReviewStart,
}

var reviewPostCmd = &cobra.Command{
	Use:   "post <pr-url> --file round.json --confirm",
	Short: "Post a review round to GitHub and save it to the ledger",
	Long: `Posts the round in --file: inline findings (anchors checked against the
diff), replies with verdicts on earlier threads, and one review event.
--confirm is required and means the user approved the exact text.

round.json:
  {"event": "COMMENT|APPROVE|REQUEST_CHANGES", "body": "...",
   "findings": [{"file": "...", "line": 42, "severity": "issue",
                 "category": "correctness", "title": "...", "body": "..."}],
   "addressing": [{"ref": "F2", "status": "addressed|partial|ignored", "note": "..."}]}`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runReviewSubmit(args[0], true) },
}

var reviewRecordCmd = &cobra.Command{
	Use:   "record <pr-url> --file round.json",
	Short: "Save a review round to the ledger without posting it",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runReviewSubmit(args[0], false) },
}

func runReviews(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	items, err := ghreview.Queue(cfg.Home, !reviewOffline)
	if err != nil {
		return err
	}
	if !reviewAll {
		var open []ghreview.QueueItem
		for _, it := range items {
			if it.Status != ghreview.QueueClosed {
				open = append(open, it)
			}
		}
		items = open
	}
	if reviewJSON {
		return printJSON(items)
	}
	if len(items) == 0 {
		fmt.Println("No reviews waiting. Run /review-asks in a thread to pick up asks from Slack.")
		return nil
	}
	now := time.Now()
	current := ""
	for _, it := range items {
		if it.Status != current {
			current = it.Status
			fmt.Printf("\n%s\n", strings.ToUpper(current))
		}
		age := ""
		if !it.Since.IsZero() {
			age = shortAge(now.Sub(it.Since))
		}
		open := ""
		if it.OpenRefs > 0 {
			open = fmt.Sprintf("  %d open finding(s)", it.OpenRefs)
		}
		fmt.Printf("  %-24s %4s  %-46s %s%s\n", it.Ref, age, clip(it.Title, 46), it.Why, open)
	}
	return nil
}

func runReviewAsk(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	ownerRepo, n, err := parsePRURL(args[0])
	if err != nil {
		return err
	}
	if askFrom == "" {
		return fmt.Errorf("--from is required")
	}
	at := time.Now().UTC()
	if askAt != "" {
		if at, err = time.Parse(time.RFC3339, askAt); err != nil {
			return fmt.Errorf("--at: %w", err)
		}
	}
	state, err := ghreview.LoadState(cfg.Home, ownerRepo, n)
	if err != nil {
		return err
	}
	if state == nil {
		state = &ghreview.State{PR: n, OwnerRepo: ownerRepo, URL: fmt.Sprintf("https://github.com/%s/pull/%d", ownerRepo, n)}
	}
	if !state.AddAsk(ghreview.Ask{From: askFrom, Where: askWhere, Link: askLink, Note: askNote, At: at}) {
		fmt.Printf("Already recorded: %s#%d from %s\n", ownerRepo, n, askFrom)
		return nil
	}
	if err := ghreview.SaveState(cfg.Home, state); err != nil {
		return err
	}
	fmt.Printf("Recorded: %s asked for %s#%d\n", askFrom, ownerRepo, n)
	return nil
}

func runReviewStart(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	p, err := ghreview.Prepare(cfg.Home, args[0])
	if err != nil {
		return err
	}
	if reviewJSON {
		return printJSON(p)
	}
	fmt.Printf("%s#%d  %s\n", p.PR.OwnerRepo, p.PR.Number, p.PR.Title)
	fmt.Printf("author %s · %d files · head %s · base %s\n", p.PR.Author.Login, len(p.PR.Files), short(p.PR.HeadSHA), short(p.BaseSHA))
	if p.SelfPR {
		fmt.Println("your own PR: GitHub allows COMMENT only")
	}
	fmt.Printf("\ncheckout   %s\ndiff       %s\n", p.Worktree, p.DiffPath)
	for _, a := range p.Asks {
		fmt.Printf("asked by   %s (%s) %s  %s\n", a.From, a.Where, a.At.Local().Format("2 Jan 15:04"), a.Link)
	}
	if p.Rounds == 0 {
		fmt.Println("\nfirst review")
		return nil
	}
	fmt.Printf("\nre-review: %d earlier round(s), last at %s on %s\n", p.Rounds, p.LastReviewedAt.Local().Format("2 Jan 15:04"), short(p.LastHeadSHA))
	switch {
	case p.LastHeadSHA == p.PR.HeadSHA:
		fmt.Println("no new commits since that round")
	case p.InterdiffPath != "":
		note := ""
		if p.Rewritten {
			note = " (history rewritten since: rebased or force-pushed)"
		}
		fmt.Printf("interdiff  %s%s\n", p.InterdiffPath, note)
	default:
		fmt.Println("last reviewed head is gone from GitHub: grade open findings against the full diff")
	}
	for _, f := range p.OpenFindings {
		fmt.Printf("open %-4s %s:%d  %s\n", f.Ref, f.File, f.Line, f.Title)
	}
	return nil
}

func runReviewSubmit(url string, post bool) error {
	cfg := requireConfig()
	if post && !reviewConfirm {
		return fmt.Errorf("--confirm is required: post only after the user approves the exact text")
	}
	if reviewFile == "" {
		return fmt.Errorf("--file is required")
	}
	ownerRepo, n, err := parsePRURL(url)
	if err != nil {
		return err
	}
	prep, err := ghreview.LoadPrep(cfg.Home, ownerRepo, n)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(reviewFile)
	if err != nil {
		return err
	}
	var r ghreview.Round
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("parse %s: %w", reviewFile, err)
	}
	r.Event = ghreview.Event(strings.ToUpper(string(r.Event)))
	if prep.SelfPR && r.Event != "" && r.Event != ghreview.EventComment {
		return fmt.Errorf("your own PR: GitHub allows COMMENT only")
	}
	res, err := ghreview.Submit(cfg.Home, prep, r, post)
	if err != nil {
		return err
	}
	if reviewJSON {
		return printJSON(res)
	}
	if post {
		fmt.Printf("Posted %d comment(s) and %d repl(ies): %s\n", res.Posted, res.Replies, res.ReviewURL)
	} else {
		fmt.Printf("Recorded round with %d finding(s) (not posted)\n", len(res.Refs))
	}
	for _, d := range res.Dropped {
		fmt.Printf("dropped (anchor not in diff): %s\n", d)
	}
	return nil
}

// parsePRURL reads https://github.com/<owner>/<repo>/pull/<n>[/...].
func parsePRURL(url string) (string, int, error) {
	_, rest, ok := strings.Cut(url, "github.com/")
	parts := strings.Split(rest, "/")
	if !ok || len(parts) < 4 || parts[2] != "pull" {
		return "", 0, fmt.Errorf("not a GitHub PR URL: %s", url)
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", 0, fmt.Errorf("not a GitHub PR URL: %s", url)
	}
	return parts[0] + "/" + parts[1], n, nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func init() {
	reviewsCmd.Flags().BoolVar(&reviewJSON, "json", false, "Machine-readable output")
	reviewsCmd.Flags().BoolVar(&reviewAll, "all", false, "Include merged and closed PRs")
	reviewsCmd.Flags().BoolVar(&reviewOffline, "offline", false, "Skip the GitHub check; use the ledger as is")

	reviewAskCmd.Flags().StringVar(&askFrom, "from", "", "Who asked")
	reviewAskCmd.Flags().StringVar(&askWhere, "where", "slack-dm", "slack-dm | slack-thread | github")
	reviewAskCmd.Flags().StringVar(&askLink, "link", "", "Permalink to the ask")
	reviewAskCmd.Flags().StringVar(&askNote, "note", "", "What they asked, one line")
	reviewAskCmd.Flags().StringVar(&askAt, "at", "", "When they asked (RFC3339); default now")

	reviewStartCmd.Flags().BoolVar(&reviewJSON, "json", false, "Machine-readable output")
	for _, c := range []*cobra.Command{reviewPostCmd, reviewRecordCmd} {
		c.Flags().StringVar(&reviewFile, "file", "", "Round JSON")
		c.Flags().BoolVar(&reviewJSON, "json", false, "Machine-readable output")
	}
	reviewPostCmd.Flags().BoolVar(&reviewConfirm, "confirm", false, "The user approved the exact text")

	reviewCmd.AddCommand(reviewAskCmd, reviewStartCmd, reviewPostCmd, reviewRecordCmd)
	rootCmd.AddCommand(reviewsCmd, reviewCmd)
}
