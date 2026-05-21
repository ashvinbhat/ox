package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// reviewComment is the wire shape for one inline comment in a batched PR
// review (POST /repos/.../pulls/<n>/reviews → comments[]).
type reviewComment struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Side     string `json:"side"`               // "RIGHT" for additions / unchanged-in-new
	StartLine int   `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body     string `json:"body"`
}

type reviewPayload struct {
	Body     string          `json:"body"`
	Event    Event           `json:"event"`
	CommitID string          `json:"commit_id,omitempty"`
	Comments []reviewComment `json:"comments"`
}

// reviewResponse captures the relevant fields gh API returns for a posted review.
type reviewResponse struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

// PostResult is what GitHub assigned to the posted review: the review ID,
// the review's HTML URL, and the per-finding comment IDs (so follow-up
// reviews can post replies on the same threads).
type PostResult struct {
	ReviewID  int64
	HTMLURL   string
	CommentID map[string]int64 // finding.Ref → GitHub comment id; empty refs are skipped
}

// Post submits a single batched review to GitHub with each finding rendered
// as an inline comment. Returns the assigned review/comment IDs.
//
// Each finding in sel.Findings should already have a stable Ref (F1, F2…)
// assigned — those refs are NOT sent to GitHub but are kept in the local
// state file so a follow-up review can correlate prior findings to the
// comment_ids returned by the GH API.
func Post(pr *PRInfo, sel *Selection) (*PostResult, error) {
	if sel == nil || len(sel.Findings) == 0 {
		return nil, fmt.Errorf("nothing to post: no findings selected")
	}

	comments := make([]reviewComment, 0, len(sel.Findings))
	for _, f := range sel.Findings {
		c := reviewComment{
			Path: f.File,
			Line: f.Line,
			Side: "RIGHT",
			Body: renderCommentBody(f),
		}
		if f.EndLine > f.Line {
			c.StartLine = f.Line
			c.StartSide = "RIGHT"
			c.Line = f.EndLine
		}
		comments = append(comments, c)
	}

	body := strings.TrimSpace(sel.GlobalComment)
	if body == "" {
		body = "Posted by ox review."
	}

	payload := reviewPayload{
		Body:     body,
		Event:    sel.Event,
		CommitID: pr.HeadSHA,
		Comments: comments,
	}
	j, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal review payload: %w", err)
	}

	endpoint := fmt.Sprintf("repos/%s/pulls/%d/reviews", pr.OwnerRepo, pr.Number)
	cmd := exec.Command("gh", "api", "-X", "POST", endpoint, "--input", "-")
	cmd.Stdin = bytes.NewReader(j)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w", asGhErr(err))
	}

	var resp reviewResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse gh api response: %w", err)
	}

	// To bind comment_ids back to the finding refs we need a second call to
	// list this review's comments. GitHub returns them in the order they
	// were submitted (matching our comments[] slice), so we zip by index.
	postedComments, err := fetchReviewComments(pr.OwnerRepo, pr.Number, resp.ID)
	if err != nil {
		// Non-fatal: the review is posted; we just couldn't map back the IDs.
		// Follow-up reviews will lose per-finding-reply capability for this run.
		fmt.Fprintf(os.Stderr, "warning: review posted (%s) but failed to fetch comment IDs for follow-up: %v\n", resp.HTMLURL, err)
	}

	commentIDs := map[string]int64{}
	for i, f := range sel.Findings {
		if f.Ref == "" {
			continue
		}
		if i < len(postedComments) {
			commentIDs[f.Ref] = postedComments[i].ID
		}
	}

	return &PostResult{
		ReviewID:  resp.ID,
		HTMLURL:   resp.HTMLURL,
		CommentID: commentIDs,
	}, nil
}

// reviewCommentResp is the subset of fields we read off a posted review
// comment when zipping comment_ids back to finding refs.
type reviewCommentResp struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
	Line int    `json:"line"`
}

func fetchReviewComments(ownerRepo string, pr int, reviewID int64) ([]reviewCommentResp, error) {
	endpoint := fmt.Sprintf("repos/%s/pulls/%d/reviews/%d/comments", ownerRepo, pr, reviewID)
	out, err := exec.Command("gh", "api", "--paginate", endpoint).Output()
	if err != nil {
		return nil, asGhErr(err)
	}
	var comments []reviewCommentResp
	if err := json.Unmarshal(out, &comments); err != nil {
		return nil, fmt.Errorf("parse review comments: %w", err)
	}
	return comments, nil
}

// PostReply posts a reply to an existing PR review comment thread.
// Used by follow-up reviews to attach addressing verdicts to the original
// finding's comment thread.
func PostReply(ownerRepo string, pr int, inReplyTo int64, body string) error {
	endpoint := fmt.Sprintf("repos/%s/pulls/%d/comments/%d/replies", ownerRepo, pr, inReplyTo)
	payload := map[string]string{"body": body}
	j, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cmd := exec.Command("gh", "api", "-X", "POST", endpoint, "--input", "-")
	cmd.Stdin = bytes.NewReader(j)
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("gh api reply: %w", asGhErr(err))
	}
	return nil
}

// AddressingReply formats an Addressing verdict as a short reply body.
func AddressingReply(a Addressing) string {
	icon := map[AddressingStatus]string{
		AddressingAddressed: "✓",
		AddressingPartial:   "⚠",
		AddressingIgnored:   "✗",
	}[a.Status]
	return fmt.Sprintf("%s **%s** (per follow-up %s review): %s\n\n_via ox review_", icon, a.Status, a.Agent, strings.TrimSpace(a.Note))
}

// renderCommentBody produces the inline-comment text for a single finding,
// prefixing severity + agent so GitHub readers can triage at a glance.
func renderCommentBody(f Finding) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**[%s · %s]** %s\n\n", f.Severity, f.Category, f.Title)
	sb.WriteString(f.Body)
	sb.WriteString("\n\n_via ox review (agent: " + f.Agent + ")_")
	return sb.String()
}

// CurrentGHUser returns the authenticated GitHub user's login via `gh api user`.
// Used to detect self-PRs so we skip posting.
func CurrentGHUser() (string, error) {
	out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return "", asGhErr(err)
	}
	return strings.TrimSpace(string(out)), nil
}
