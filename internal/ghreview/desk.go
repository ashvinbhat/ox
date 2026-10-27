package ghreview

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/gitutil"
)

// Prep is what a review thread starts from: a full checkout of the repo at
// the PR head (so the review reads the codebase, not just the diff), the PR
// diff, and on a re-review the diff since the last reviewed head plus every
// finding still open.
type Prep struct {
	PR             *PRInfo   `json:"pr"`
	RepoName       string    `json:"repoName"`
	Worktree       string    `json:"worktree"`
	DiffPath       string    `json:"diffPath"`
	BaseSHA        string    `json:"baseSHA,omitempty"`
	SelfPR         bool      `json:"selfPR"`
	Asks           []Ask     `json:"asks,omitempty"`
	Rounds         int       `json:"rounds"`
	LastHeadSHA    string    `json:"lastHeadSHA,omitempty"`
	LastReviewedAt time.Time `json:"lastReviewedAt,omitzero"`
	InterdiffPath  string    `json:"interdiffPath,omitempty"`
	Rewritten      bool      `json:"rewritten,omitempty"` // last reviewed head is not an ancestor of the head: rebased or force-pushed
	OpenFindings   []Finding `json:"openFindings,omitempty"`
}

// WorkDir holds a PR's diff files and prepared context. It sits outside the
// worktree so review files never make the checkout look dirty to prune.
func WorkDir(oxHome, ownerRepo string, pr int) string {
	return filepath.Join(oxHome, "reviews", "work", fmt.Sprintf("%s-%d", strings.ReplaceAll(ownerRepo, "/", "-"), pr))
}

// Prepare resolves the PR, checks out its head in ~/.ox/worktrees/<repo>/review-<n>,
// writes the diff (and the interdiff on a re-review) to WorkDir, and saves
// the context Submit needs.
func Prepare(oxHome, ref string) (*Prep, error) {
	if err := CheckGHAuth(); err != nil {
		return nil, err
	}
	pr, err := ResolvePR(ref, "")
	if err != nil {
		return nil, err
	}
	repoName := pr.OwnerRepo[strings.LastIndex(pr.OwnerRepo, "/")+1:]
	wt, err := CreateReviewWorktree(oxHome, repoName, pr)
	if err != nil {
		return nil, err
	}
	repoPath := filepath.Join(oxHome, "repos", repoName)

	dir := WorkDir(oxHome, pr.OwnerRepo, pr.Number)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	diff, err := Diff(pr)
	if err != nil {
		return nil, err
	}
	p := &Prep{PR: pr, RepoName: repoName, Worktree: wt.Path, DiffPath: filepath.Join(dir, "diff.patch")}
	if err := os.WriteFile(p.DiffPath, []byte(diff), 0o644); err != nil {
		return nil, err
	}
	if base, err := gitutil.RunOutput(repoPath, "merge-base", wt.Branch, wt.BaseRef); err == nil {
		p.BaseSHA = strings.TrimSpace(base)
	}
	if me, err := CurrentGHUser(); err == nil && me == pr.Author.Login {
		p.SelfPR = true
	}

	state, err := LoadState(oxHome, pr.OwnerRepo, pr.Number)
	if err != nil {
		return nil, err
	}
	if state != nil {
		p.Asks = state.Asks
		p.Rounds = len(state.History)
		p.OpenFindings = state.OpenFindings()
		if last := state.LastReview(); last != nil {
			p.LastHeadSHA, p.LastReviewedAt = last.HeadSHA, last.ReviewedAt
			if last.HeadSHA != "" && last.HeadSHA != pr.HeadSHA {
				p.writeInterdiff(repoPath, dir)
			}
		}
	}

	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "context.json"), data, 0o644); err != nil {
		return nil, err
	}
	return p, nil
}

// writeInterdiff diffs the last reviewed head against the current one,
// limited to the files the PR touches so merges from the base stay out.
// An old head GitHub no longer serves (force-pushed away) leaves no interdiff;
// the review then grades open findings against the full diff.
func (p *Prep) writeInterdiff(repoPath, dir string) {
	if gitutil.Run(repoPath, "cat-file", "-e", p.LastHeadSHA+"^{commit}") != nil {
		_ = gitutil.Run(repoPath, "fetch", "origin", p.LastHeadSHA)
	}
	if gitutil.Run(repoPath, "cat-file", "-e", p.LastHeadSHA+"^{commit}") != nil {
		return
	}
	p.Rewritten = gitutil.Run(repoPath, "merge-base", "--is-ancestor", p.LastHeadSHA, p.PR.HeadSHA) != nil
	args := []string{"diff", "--no-color", p.LastHeadSHA, p.PR.HeadSHA, "--"}
	for _, f := range p.PR.Files {
		args = append(args, f.Path)
	}
	out, err := gitutil.RunOutput(repoPath, args...)
	if err != nil {
		return
	}
	p.InterdiffPath = filepath.Join(dir, "interdiff.patch")
	if os.WriteFile(p.InterdiffPath, []byte(out), 0o644) != nil {
		p.InterdiffPath = ""
	}
}

// LoadPrep reads the context Prepare saved for a PR.
func LoadPrep(oxHome, ownerRepo string, pr int) (*Prep, error) {
	b, err := os.ReadFile(filepath.Join(WorkDir(oxHome, ownerRepo, pr), "context.json"))
	if err != nil {
		return nil, fmt.Errorf("no prepared review for %s#%d: run `ox review start` first", ownerRepo, pr)
	}
	var p Prep
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Round is one review round as the reviewer writes it: inline findings,
// verdicts on open findings, the event and an optional body.
type Round struct {
	Event      Event        `json:"event"`
	Body       string       `json:"body,omitempty"`
	Findings   []Finding    `json:"findings,omitempty"`
	Addressing []Addressing `json:"addressing,omitempty"`
}

// SubmitResult reports what Submit saved and, when posting, what reached GitHub.
type SubmitResult struct {
	ReviewURL string   `json:"reviewURL,omitempty"`
	Posted    int      `json:"posted"`
	Replies   int      `json:"replies"`
	Refs      []string `json:"refs"`
	Dropped   []string `json:"dropped,omitempty"` // findings whose anchor is not in the diff
}

// Submit saves a round to the ledger. With post it first sends the review to
// GitHub: anchors are checked against the diff (unanchorable findings are
// dropped, not posted) and verdicts go out as replies on the earlier threads.
// Posting is the caller's to gate on the user's approval of the exact text.
func Submit(oxHome string, prep *Prep, r Round, post bool) (*SubmitResult, error) {
	state, err := LoadState(oxHome, prep.PR.OwnerRepo, prep.PR.Number)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &State{PR: prep.PR.Number, OwnerRepo: prep.PR.OwnerRepo}
	}
	state.URL, state.Title, state.Author = prep.PR.URL, prep.PR.Title, prep.PR.Author.Login

	res := &SubmitResult{}
	findings := r.Findings
	if post {
		diff, err := os.ReadFile(prep.DiffPath)
		if err != nil {
			return nil, fmt.Errorf("diff file gone: run `ox review start` again")
		}
		var dropped []Finding
		findings, dropped = ParseDiff(string(diff)).FilterFindings(findings)
		for _, d := range dropped {
			res.Dropped = append(res.Dropped, fmt.Sprintf("%s:%d %s", d.File, d.Line, d.Title))
		}
	}
	offset := state.findingCount()
	for i := range findings {
		findings[i].Ref = fmt.Sprintf("F%d", offset+i+1)
		res.Refs = append(res.Refs, findings[i].Ref)
	}

	rec := ReviewRecord{
		ReviewedAt: time.Now().UTC(), HeadSHA: prep.PR.HeadSHA, BaseSHA: prep.BaseSHA,
		Findings: findings, Addressing: r.Addressing,
	}
	if post {
		priorByRef := map[string]Finding{}
		for _, rec := range state.History {
			for _, f := range rec.Findings {
				priorByRef[f.Ref] = f
			}
		}
		event := r.Event
		if event == "" {
			event = EventComment
		}
		posted, err := Post(prep.PR, &Selection{Findings: findings, Event: event, GlobalComment: r.Body})
		if err != nil {
			return nil, err
		}
		for i := range findings {
			findings[i].CommentID = posted.CommentID[findings[i].Ref]
		}
		rec.ReviewID, rec.ReviewURL, rec.Posted = posted.ReviewID, posted.HTMLURL, true
		res.ReviewURL, res.Posted = posted.HTMLURL, len(findings)
		for _, a := range r.Addressing {
			prior, ok := priorByRef[a.Ref]
			if !ok || prior.CommentID == 0 {
				continue
			}
			if PostReply(prep.PR.OwnerRepo, prep.PR.Number, prior.CommentID, AddressingReply(a)) == nil {
				res.Replies++
			}
		}
	}
	state.AppendRecord(rec)
	state.Head, state.PRState = prep.PR.HeadSHA, prep.PR.State
	return res, SaveState(oxHome, state)
}

// Queue statuses, in the order a reviewer works them.
const (
	QueueToReview = "to review"
	QueueReReview = "re-review"
	QueueWaiting  = "waiting on author"
	QueueClosed   = "closed"
)

// QueueItem is one PR in the review queue.
type QueueItem struct {
	Ref      string    `json:"ref"`
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	Status   string    `json:"status"`
	Why      string    `json:"why"`
	Since    time.Time `json:"since"`
	OpenRefs int       `json:"openFindings"`
}

// Status places a PR in the queue from what the ledger knows.
func (s *State) Status() (status, why string, since time.Time) {
	last, ask := s.LastReview(), s.LatestAsk()
	switch {
	case s.PRState == "MERGED" || s.PRState == "CLOSED":
		return QueueClosed, strings.ToLower(s.PRState), time.Time{}
	case last == nil && ask != nil:
		return QueueToReview, "asked by " + ask.From + " (" + ask.Where + ")", ask.At
	case last == nil:
		return QueueToReview, "no round recorded", time.Time{}
	case ask != nil && ask.At.After(last.ReviewedAt):
		return QueueReReview, "asked again by " + ask.From + " (" + ask.Where + ")", ask.At
	case s.Head != "" && s.Head != last.HeadSHA:
		return QueueReReview, "new commits since your review", last.ReviewedAt
	}
	return QueueWaiting, "reviewed, no new commits", last.ReviewedAt
}

// Queue lists the ledger's PRs by status. With refresh it first asks GitHub
// for each open PR's state and head; closed PRs are never asked about again.
func Queue(oxHome string, refresh bool) ([]QueueItem, error) {
	states, err := ListStates(oxHome)
	if err != nil {
		return nil, err
	}
	var items []QueueItem
	for _, s := range states {
		if refresh && s.PRState != "MERGED" && s.PRState != "CLOSED" {
			if refreshState(s) {
				_ = SaveState(oxHome, s)
			}
		}
		status, why, since := s.Status()
		ref := fmt.Sprintf("%s#%d", s.OwnerRepo[strings.LastIndex(s.OwnerRepo, "/")+1:], s.PR)
		items = append(items, QueueItem{
			Ref: ref, URL: s.URL, Title: s.Title, Author: s.Author,
			Status: status, Why: why, Since: since, OpenRefs: len(s.OpenFindings()),
		})
	}
	order := map[string]int{QueueToReview: 0, QueueReReview: 1, QueueWaiting: 2, QueueClosed: 3}
	sort.SliceStable(items, func(i, j int) bool {
		if order[items[i].Status] != order[items[j].Status] {
			return order[items[i].Status] < order[items[j].Status]
		}
		return items[i].Since.Before(items[j].Since)
	})
	return items, nil
}

func refreshState(s *State) bool {
	pr, err := ResolvePR(fmt.Sprint(s.PR), s.OwnerRepo)
	if err != nil {
		return false
	}
	s.PRState, s.Head = pr.State, pr.HeadSHA
	s.URL, s.Title, s.Author = pr.URL, pr.Title, pr.Author.Login
	return true
}
