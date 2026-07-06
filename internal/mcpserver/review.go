package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ashvinbhat/ox/internal/ghreview"
	"github.com/ashvinbhat/ox/internal/harness"
)

// reviewContext is the mission-local pointer written by prepare_review and
// consumed by post_review, so the two calls can span server restarts.
type reviewContext struct {
	PR       *ghreview.PRInfo `json:"pr"`
	RepoName string           `json:"repo_name"`
	Worktree string           `json:"worktree"`
	DiffPath string           `json:"diff_path"`
}

type prepareReviewIn struct {
	PR string `json:"pr" jsonschema:"PR URL, number, or branch"`
}

type priorFinding struct {
	Ref      string `json:"ref"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Posted   bool   `json:"posted"` // has a GitHub comment thread to reply to
}

type prepareReviewOut struct {
	Number        int            `json:"number"`
	Title         string         `json:"title"`
	Author        string         `json:"author"`
	OwnerRepo     string         `json:"owner_repo"`
	HeadSHA       string         `json:"head_sha"`
	SelfPR        bool           `json:"self_pr" jsonschema:"true when you authored the PR — approve/request-changes are impossible, only COMMENT"`
	Worktree      string         `json:"worktree" jsonschema:"checkout of the PR head — point review jobs here via cwd"`
	DiffPath      string         `json:"diff_path" jsonschema:"unified diff file — give jobs this path (it is inside the worktree add_dirs scope)"`
	Files         []string       `json:"files"`
	Followup      bool           `json:"followup" jsonschema:"a prior review round exists — jobs must also grade prior findings"`
	PriorFindings []priorFinding `json:"prior_findings,omitempty"`
}

type postFindingIn struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	EndLine  int    `json:"end_line,omitempty"`
	Severity string `json:"severity" jsonschema:"blocker|issue|suggest|nit"`
	Category string `json:"category" jsonschema:"security|correctness|design|test|naming|perf|docs"`
	Agent    string `json:"agent,omitempty"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

type postAddressingIn struct {
	Ref    string `json:"ref" jsonschema:"prior finding ref (F1, F2...)"`
	Status string `json:"status" jsonschema:"addressed|partial|ignored"`
	Note   string `json:"note"`
}

type postReviewIn struct {
	Event       string             `json:"event" jsonschema:"COMMENT | APPROVE | REQUEST_CHANGES"`
	Body        string             `json:"body,omitempty" jsonschema:"optional top-level review body"`
	Findings    []postFindingIn    `json:"findings,omitempty" jsonschema:"inline comments — only findings the user kept"`
	Addressings []postAddressingIn `json:"addressings,omitempty" jsonschema:"follow-up verdicts posted as replies on prior threads"`
	Confirm     bool               `json:"confirm" jsonschema:"must be true — posts to GitHub. Requires explicit user approval of the exact set first."`
}

type postReviewOut struct {
	ReviewURL string            `json:"review_url,omitempty"`
	Posted    int               `json:"posted"`
	Replies   int               `json:"replies"`
	Refs      map[string]string `json:"refs,omitempty" jsonschema:"finding title → assigned ref"`
	Dropped   []string          `json:"dropped,omitempty" jsonschema:"findings rejected because their anchor is not in the diff"`
}

func (s *Server) registerReviewTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "prepare_review",
		Description: "Set up a PR review: resolves the PR, checks out its head in a review worktree, saves the diff, and loads prior review rounds (follow-up state).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in prepareReviewIn) (*mcp.CallToolResult, prepareReviewOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, prepareReviewOut{}, err
		}
		if err := ghreview.CheckGHAuth(); err != nil {
			return nil, prepareReviewOut{}, err
		}

		pr, err := ghreview.ResolvePR(in.PR, "")
		if err != nil {
			return nil, prepareReviewOut{}, err
		}

		repoName := ""
		for name := range s.cfg.Repos {
			if strings.EqualFold(name, pr.HeadRepo.Name) {
				repoName = name
				break
			}
		}
		if repoName == "" {
			return nil, prepareReviewOut{}, fmt.Errorf("no registered repo matches %q — run 'ox repo add' first", pr.HeadRepo.Name)
		}

		wt, err := ghreview.CreateReviewWorktree(s.oxHome, repoName, pr)
		if err != nil {
			return nil, prepareReviewOut{}, err
		}

		diff, err := ghreview.Diff(pr)
		if err != nil {
			return nil, prepareReviewOut{}, err
		}
		diffPath := filepath.Join(wt.Path, ".ox-review-diff.patch")
		if err := os.WriteFile(diffPath, []byte(diff), 0o644); err != nil {
			return nil, prepareReviewOut{}, err
		}

		rc := reviewContext{PR: pr, RepoName: repoName, Worktree: wt.Path, DiffPath: diffPath}
		data, _ := json.MarshalIndent(rc, "", "  ")
		if err := os.WriteFile(filepath.Join(m.Dir(), "review.json"), data, 0o644); err != nil {
			return nil, prepareReviewOut{}, err
		}

		out := prepareReviewOut{
			Number: pr.Number, Title: pr.Title, Author: pr.Author.Login,
			OwnerRepo: pr.OwnerRepo, HeadSHA: pr.HeadSHA,
			Worktree: wt.Path, DiffPath: diffPath,
		}
		if me, err := ghreview.CurrentGHUser(); err == nil && me == pr.Author.Login {
			out.SelfPR = true
		}
		for _, f := range pr.Files {
			out.Files = append(out.Files, f.Path)
		}

		state, err := ghreview.LoadState(s.oxHome, pr.OwnerRepo, pr.Number)
		if err == nil && state != nil && len(state.History) > 0 {
			out.Followup = true
			// Addressing-only rounds carry no findings — walk back to the
			// most recent round that does, so priors survive reply rounds.
			for i := len(state.History) - 1; i >= 0; i-- {
				if len(state.History[i].Findings) == 0 {
					continue
				}
				for _, f := range state.History[i].Findings {
					out.PriorFindings = append(out.PriorFindings, priorFinding{
						Ref: f.Ref, File: f.File, Line: f.Line,
						Severity: string(f.Severity), Title: f.Title, Posted: f.CommentID != 0,
					})
				}
				break
			}
		}

		m.AppendEvent("review_prepared", s.actor(), map[string]any{
			"pr": pr.URL, "followup": out.Followup, "files": len(out.Files),
		})
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "post_review",
		Description: "Post the review to GitHub: inline comments for kept findings, replies for follow-up verdicts, one review event. HARD GATE: the user must approve the exact set first. Anchors are validated against the diff — unanchorable findings are dropped, not posted.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in postReviewIn) (*mcp.CallToolResult, postReviewOut, error) {
		if !in.Confirm {
			return nil, postReviewOut{}, fmt.Errorf("set confirm=true after the user approves the exact findings to post")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, postReviewOut{}, err
		}

		var rc reviewContext
		data, err := os.ReadFile(filepath.Join(m.Dir(), "review.json"))
		if err != nil {
			return nil, postReviewOut{}, fmt.Errorf("no prepared review — call prepare_review first")
		}
		if err := json.Unmarshal(data, &rc); err != nil {
			return nil, postReviewOut{}, err
		}

		event := ghreview.Event(strings.ToUpper(in.Event))
		if event != ghreview.EventComment && event != ghreview.EventApprove && event != ghreview.EventRequestChanges {
			return nil, postReviewOut{}, fmt.Errorf("event must be COMMENT, APPROVE, or REQUEST_CHANGES")
		}
		if me, err := ghreview.CurrentGHUser(); err == nil && me == rc.PR.Author.Login && event != ghreview.EventComment {
			return nil, postReviewOut{}, fmt.Errorf("self-PR: GitHub only allows COMMENT on your own PR")
		}

		var findings []ghreview.Finding
		for _, f := range in.Findings {
			findings = append(findings, ghreview.Finding{
				File: f.File, Line: f.Line, EndLine: f.EndLine,
				Severity: ghreview.Severity(f.Severity), Category: ghreview.Category(f.Category),
				Agent: f.Agent, Title: f.Title, Body: f.Body,
			})
		}

		// Anchor validation against the actual diff — the lesson from the 422 era.
		diffData, err := os.ReadFile(rc.DiffPath)
		if err != nil {
			return nil, postReviewOut{}, fmt.Errorf("diff file gone — re-run prepare_review")
		}
		diffMap := ghreview.ParseDiff(string(diffData))
		postable, droppedFindings := diffMap.FilterFindings(findings)

		// Refs must exist BEFORE posting: Post correlates GitHub comment IDs
		// back to findings by ref, and follow-up replies depend on those IDs.
		for i := range postable {
			postable[i].Ref = fmt.Sprintf("F%d", i+1)
		}

		out := postReviewOut{}
		for _, d := range droppedFindings {
			out.Dropped = append(out.Dropped, fmt.Sprintf("%s:%d %s", d.File, d.Line, d.Title))
		}

		state, _ := ghreview.LoadState(s.oxHome, rc.PR.OwnerRepo, rc.PR.Number)
		priorByRef := map[string]ghreview.Finding{}
		if state != nil {
			for i := len(state.History) - 1; i >= 0; i-- {
				if len(state.History[i].Findings) == 0 {
					continue
				}
				for _, f := range state.History[i].Findings {
					priorByRef[f.Ref] = f
				}
				break
			}
		}

		var addr []ghreview.Addressing
		for _, a := range in.Addressings {
			addr = append(addr, ghreview.Addressing{Ref: a.Ref, Status: ghreview.AddressingStatus(a.Status), Note: a.Note})
		}

		sel := &ghreview.Selection{
			Findings: postable, Event: event, GlobalComment: in.Body,
			Addressing: addr, PriorByRef: priorByRef,
		}

		result, err := ghreview.Post(rc.PR, sel)
		if err != nil {
			return nil, postReviewOut{}, err
		}
		out.ReviewURL = result.HTMLURL
		out.Posted = len(postable)

		for _, a := range addr {
			prior, ok := priorByRef[a.Ref]
			if !ok || prior.CommentID == 0 {
				continue
			}
			if err := ghreview.PostReply(rc.PR.OwnerRepo, rc.PR.Number, prior.CommentID, ghreview.AddressingReply(a)); err == nil {
				out.Replies++
			}
		}

		// Persist the round: refs assigned, comment IDs captured for follow-ups.
		if state == nil {
			state = &ghreview.State{PR: rc.PR.Number, OwnerRepo: rc.PR.OwnerRepo}
		}
		for i := range postable {
			if id, ok := result.CommentID[postable[i].Ref]; ok {
				postable[i].CommentID = id
			}
		}
		rec := ghreview.ReviewRecord{
			HeadSHA: rc.PR.HeadSHA, ReviewID: result.ReviewID, ReviewURL: result.HTMLURL,
			Posted: true, Findings: postable,
		}
		state.AppendRecord(rec)
		if err := ghreview.SaveState(s.oxHome, state); err == nil {
			out.Refs = map[string]string{}
			if last := state.LastReview(); last != nil {
				for _, f := range last.Findings {
					out.Refs[f.Title] = f.Ref
				}
			}
		}

		harness.LinkPR(m, rc.RepoName, rc.PR.URL)
		m.AppendEvent("review_posted", s.actor(), map[string]any{
			"pr": rc.PR.URL, "posted": out.Posted, "replies": out.Replies, "url": out.ReviewURL,
		})
		return nil, out, nil
	})
}
