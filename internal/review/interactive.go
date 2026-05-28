package review

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Event mirrors the GitHub review event enum: COMMENT, APPROVE, REQUEST_CHANGES.
type Event string

const (
	EventComment        Event = "COMMENT"
	EventApprove        Event = "APPROVE"
	EventRequestChanges Event = "REQUEST_CHANGES"
)

// Selection is the user's interactive choices: which findings to post, what
// review-event type, an optional final global comment, and (for follow-up
// reviews) which addressing verdicts to post as replies on prior comment
// threads.
type Selection struct {
	Findings      []Finding
	Event         Event
	GlobalComment string

	// Addressing verdicts the user accepted for posting as replies. PriorByRef
	// maps the prior finding ref to its CommentID so the caller can post
	// each reply with PostReply(in_reply_to=...).
	Addressing  []Addressing
	PriorByRef  map[string]Finding // for the caller's posting loop (ref → prior finding incl. CommentID)
}

// RunInteractive presents the review to the user and walks them through:
//   1. (follow-up only) which addressing verdicts to post as replies to the prior comments
//   2. which new findings to post as inline comments
//   3. the review event type (comment / request-changes / approve)
//   4. an optional final global comment
//   5. final preview + explicit y/N confirmation
//
// addressing + priorByRef may be empty/nil for a first-time review.
// priorByRef maps a prior finding's Ref to the prior Finding (so its
// CommentID can be used as in_reply_to when posting the reply).
//
// worktree is needed so `chat <n>` / `ask <n>` can launch claude with the
// PR head checkout as cwd. May be nil if the caller has already torn down
// the worktree — chat/ask will surface an error in that case.
//
// Returns a Selection with empty fields if the user cancels at any prompt.
func RunInteractive(findings []Finding, addressing []Addressing, priorByRef map[string]Finding, worktree *ReviewWorktree, in io.Reader, out io.Writer) (*Selection, error) {
	if len(findings) == 0 && len(addressing) == 0 {
		return &Selection{}, nil
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Step 1: addressing replies (follow-up only).
	chosenAddressing, bareReviewFromAddressing, err := selectAddressing(addressing, priorByRef, worktree, scanner, out)
	if err != nil {
		return nil, err
	}
	// User shortcut "approve" / "request-changes" / "comment" at the
	// addressing prompt → skip the findings prompt entirely and use the
	// bare-review Selection as-is.
	if bareReviewFromAddressing != nil {
		return bareReviewFromAddressing, nil
	}

	if len(findings) == 0 {
		// Follow-up with no new findings — go straight to the preview.
		sel := &Selection{
			Addressing: chosenAddressing,
			PriorByRef: priorByRef,
		}
		if len(chosenAddressing) == 0 {
			return &Selection{}, nil
		}
		// Still ask for event + global since the user may want to record an overall verdict.
		sel.Event = promptEvent(scanner, out)
		sel.GlobalComment = promptGlobalComment(scanner, out)
		if !confirmPost(sel, scanner, out) {
			return &Selection{}, nil
		}
		return sel, nil
	}

	// Step 2: new-findings selection.
	//
	// IMPORTANT: sort `working` ONCE here so the display indices (which
	// RenderSummary emits in severity order) align with how this function
	// indexes back into `working[idx-1]` for selection / expand / edit /
	// chat. Without this, the user selects "2" but gets unsorted[1],
	// which is a completely different finding from sorted[1].
	working := sortFindings(findings)

	// Show the summary view up front so the user sees titles + anchors and
	// can drill into bodies on demand with `e <n>` / `expand <n>` / `view <n>`.
	RenderSummary(out, working)

	var picks []int
	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Each finding shows a 3-line excerpt. Drill in with `e <n>`, talk to a reviewer with `chat <n>` (finding) or `chat` (review-wide).")
		fmt.Fprintln(out, "Post which to GitHub? [1,3,5-7 | all | none | approve | request-changes | comment | e <n> | chat [<n>] | ask [<n>] <q> | edit <n> | help]")
		fmt.Fprint(out, "> ")
		if !scanner.Scan() {
			return nil, scanner.Err()
		}
		input := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(input)
		switch {
		case input == "" || strings.EqualFold(input, "none"):
			return &Selection{}, nil
		case strings.EqualFold(input, "approve"),
			strings.EqualFold(input, "request-changes"), strings.EqualFold(input, "request_changes"),
			strings.EqualFold(input, "comment"):
			// Bare-review path: post a review with the given event and NO
			// inline comments. Useful when you've read all the findings and
			// just want to approve / comment / request changes without picking
			// any of them to send as inline comments.
			//
			// Preserve any addressing entries already picked at the addressing
			// prompt — bare-review at the findings prompt should still post
			// those replies. (Otherwise the user has to choose between
			// addressing-replies and approve.)
			var event Event
			switch lower {
			case "approve":
				event = EventApprove
			case "request-changes", "request_changes":
				event = EventRequestChanges
			case "comment":
				event = EventComment
			}
			sel, err := runBareReview(event, scanner, out)
			if err != nil || sel == nil {
				return sel, err
			}
			if len(chosenAddressing) > 0 {
				sel.Addressing = chosenAddressing
				sel.PriorByRef = priorByRef
			}
			return sel, nil
		case strings.EqualFold(input, "help") || input == "?":
			fmt.Fprintln(out, "  1,3,5-7 / all / none      select findings to post")
			fmt.Fprintln(out, "  approve                   post a bare APPROVE review (no inline comments)")
			fmt.Fprintln(out, "  request-changes           post a bare REQUEST_CHANGES review (no inline comments)")
			fmt.Fprintln(out, "  comment                   post a bare COMMENT review (no inline comments)")
			fmt.Fprintln(out, "  e 2 / expand 2 / view 2   show finding [2] in full")
			fmt.Fprintln(out, "  chat                      chat with the lead reviewer about the WHOLE review")
			fmt.Fprintln(out, "  chat 2                    chat about finding [2] specifically")
			fmt.Fprintln(out, "  ask <question>            one-shot Q&A with the lead reviewer (review-wide)")
			fmt.Fprintln(out, "  ask 2 <question>          one-shot Q&A about finding [2] specifically")
			fmt.Fprintln(out, "  edit 2                    edit finding [2]'s body in $EDITOR")
			continue
		case strings.HasPrefix(lower, "e ") || strings.HasPrefix(lower, "expand ") || strings.HasPrefix(lower, "view "):
			arg := strings.TrimSpace(input[strings.IndexByte(input, ' ')+1:])
			n, err := strconv.Atoi(arg)
			if err != nil || n < 1 || n > len(working) {
				fmt.Fprintf(out, "  invalid finding index: %q (expected 1..%d)\n", arg, len(working))
				continue
			}
			fmt.Fprintln(out)
			RenderOne(out, working[n-1], n)
			continue
		case lower == "chat" || strings.HasPrefix(lower, "chat ") ||
			lower == "ask" || strings.HasPrefix(lower, "ask "):
			if worktree == nil {
				fmt.Fprintln(out, "  chat/ask unavailable: review worktree has been torn down")
				continue
			}
			isAsk := lower == "ask" || strings.HasPrefix(lower, "ask ")

			// Parse `chat` / `chat <n>` / `ask <q>` / `ask <n> <q>`. The
			// distinction between finding-specific and review-wide is
			// whether the first token after `chat`/`ask` is a number.
			var rest string
			if sp := strings.IndexByte(input, ' '); sp >= 0 {
				rest = strings.TrimSpace(input[sp+1:])
			}
			firstTok, remainder, _ := strings.Cut(rest, " ")
			n, indexErr := strconv.Atoi(strings.TrimSpace(firstTok))

			if rest == "" || indexErr != nil {
				// Review-wide (no index given).
				question := ""
				if isAsk {
					if strings.TrimSpace(rest) == "" {
						fmt.Fprintln(out, "  ask requires a question: `ask what's the highest-priority finding?`")
						continue
					}
					question = rest
				}
				fmt.Fprintln(out)
				if isAsk {
					fmt.Fprintln(out, "── asking the lead reviewer (review-wide) ──")
				} else {
					fmt.Fprintln(out, "── chat with the lead reviewer (review-wide) — type /exit or Ctrl-D when done ──")
				}
				fmt.Fprintln(out)
				if cerr := ChatAboutReview(worktree, question); cerr != nil {
					fmt.Fprintf(out, "  chat/ask failed: %v\n", cerr)
				}
				fmt.Fprintln(out, "\n── back to selection ──")
				continue
			}

			// Finding-specific: `chat <n>` / `ask <n> <q>`.
			if n < 1 || n > len(working) {
				fmt.Fprintf(out, "  invalid finding index: %q (expected 1..%d)\n", firstTok, len(working))
				continue
			}
			question := ""
			if isAsk {
				if strings.TrimSpace(remainder) == "" {
					fmt.Fprintln(out, "  ask requires a question: `ask <n> what does this mean?`")
					continue
				}
				question = strings.TrimSpace(remainder)
			}
			finding := working[n-1]
			fmt.Fprintln(out)
			if isAsk {
				fmt.Fprintf(out, "── asking about finding [%d] (%s) ──\n\n", n, finding.Agent)
			} else {
				fmt.Fprintf(out, "── chat about finding [%d] (%s) — type /exit or Ctrl-D when done ──\n\n", n, finding.Agent)
			}
			if cerr := ChatAboutFinding(worktree, finding, question); cerr != nil {
				fmt.Fprintf(out, "  chat/ask failed: %v\n", cerr)
			}
			fmt.Fprintln(out, "\n── back to selection ──")
			continue
		case strings.HasPrefix(lower, "edit "):
			arg := strings.TrimSpace(input[len("edit "):])
			n, err := strconv.Atoi(arg)
			if err != nil || n < 1 || n > len(working) {
				fmt.Fprintf(out, "  invalid finding index: %q (expected 1..%d)\n", arg, len(working))
				continue
			}
			edited, eerr := EditBody(working[n-1].Body)
			if eerr != nil {
				fmt.Fprintf(out, "  edit failed: %v\n", eerr)
				continue
			}
			working[n-1].Body = edited
			fmt.Fprintf(out, "  edited finding [%d]\n", n)
			continue
		}

		selected, err := ParseSelection(input, len(working))
		if err != nil {
			fmt.Fprintf(out, "  %v\n", err)
			continue
		}
		picks = selected
		break
	}

	if len(picks) == 0 {
		return &Selection{}, nil
	}

	chosen := make([]Finding, 0, len(picks))
	for _, idx := range picks {
		chosen = append(chosen, working[idx-1])
	}

	// Ask for review event.
	event := promptEvent(scanner, out)

	// Ask for optional global comment.
	global := promptGlobalComment(scanner, out)

	sel := &Selection{
		Findings:      chosen,
		Event:         event,
		GlobalComment: global,
		Addressing:    chosenAddressing,
		PriorByRef:    priorByRef,
	}

	// Final preview + explicit confirmation. Nothing gets posted unless the
	// user says yes here — pressing enter, "n", Ctrl-D all abort safely.
	if !confirmPost(sel, scanner, out) {
		return &Selection{}, nil
	}
	return sel, nil
}

// selectAddressing renders aggregated addressing verdicts (one prior
// finding may have multiple agent verdicts) and asks the user to pick
// which to post as replies. Each selected entry produces one reply on the
// prior finding's comment thread.
//
// Returns (chosen, bareReview, error):
//   - chosen != nil and bareReview == nil: user picked addressing entries,
//     caller should continue to findings selection.
//   - chosen == nil and bareReview == nil: user typed "none" / cancelled,
//     caller should continue to findings selection.
//   - bareReview != nil: user shortcut to a bare-review event (approve /
//     comment / request-changes) at the addressing prompt; caller should
//     finalize the review with bareReview as the Selection and skip the
//     findings prompt entirely.
//
// Rendering is delegated to RenderAddressing so the look matches the
// non-interactive summary exactly (and so the [N] indices the prompt
// expects line up with what the user sees).
//
// worktree is needed so `chat` / `ask <q>` can launch claude with the PR
// head checkout as cwd. nil disables those commands.
func selectAddressing(addressing []Addressing, priorByRef map[string]Finding, worktree *ReviewWorktree, scanner *bufio.Scanner, out io.Writer) ([]Addressing, *Selection, error) {
	addressingWorktree := worktree
	if len(addressing) == 0 {
		return nil, nil, nil
	}

	flat := RenderAddressing(out, addressing, priorByRef, true /* withIndices */)

	for {
		fmt.Fprintln(out, "\nPost which addressing verdicts as replies on the prior comments?")
		fmt.Fprintln(out, "[1,3,5-7 | all | none | approve | request-changes | comment | chat | ask <q> | help]")
		fmt.Fprint(out, "> ")
		if !scanner.Scan() {
			return nil, nil, scanner.Err()
		}
		input := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(input)
		switch {
		case input == "" || strings.EqualFold(input, "none"):
			return nil, nil, nil
		case strings.EqualFold(input, "help") || input == "?":
			fmt.Fprintln(out, "  1,3,5-7 / all / none     select addressing verdicts to post as replies")
			fmt.Fprintln(out, "  approve                  shortcut: post a bare APPROVE review (no addressing replies, no inline findings)")
			fmt.Fprintln(out, "  request-changes          shortcut: post a bare REQUEST_CHANGES review")
			fmt.Fprintln(out, "  comment                  shortcut: post a bare COMMENT review")
			fmt.Fprintln(out, "  chat                     chat with the lead reviewer (review-wide)")
			fmt.Fprintln(out, "  ask <question>           one-shot Q&A with the lead reviewer (review-wide)")
			continue
		case lower == "approve",
			lower == "request-changes", lower == "request_changes",
			lower == "comment":
			// Bare-review shortcut from the addressing prompt — skip both
			// the rest of the addressing selection AND the findings prompt
			// entirely. No addressing replies, no inline findings.
			var event Event
			switch lower {
			case "approve":
				event = EventApprove
			case "request-changes", "request_changes":
				event = EventRequestChanges
			case "comment":
				event = EventComment
			}
			sel, err := runBareReview(event, scanner, out)
			return nil, sel, err
		case lower == "chat":
			runReviewChat(addressingWorktree, "", out)
			continue
		case strings.HasPrefix(lower, "ask "):
			q := strings.TrimSpace(input[4:])
			if q == "" {
				fmt.Fprintln(out, "  ask requires a question: `ask what's the highest-priority?`")
				continue
			}
			runReviewChat(addressingWorktree, q, out)
			continue
		}
		picks, err := ParseSelection(input, len(flat))
		if err != nil {
			fmt.Fprintf(out, "  %v\n", err)
			continue
		}
		chosen := make([]Addressing, 0, len(picks))
		for _, i := range picks {
			a := flat[i-1]
			// Skip if we don't have a comment ID to reply to (the prior
			// finding was never posted to GitHub — e.g. self-PR history).
			if prior, ok := priorByRef[a.Ref]; !ok || prior.CommentID == 0 {
				fmt.Fprintf(out, "  skipping addressing %s — no GitHub comment to reply to\n", a.Ref)
				continue
			}
			chosen = append(chosen, a)
		}
		return chosen, nil, nil
	}
}

// runReviewChat is a small wrapper around ChatAboutReview shared by both
// the addressing-selection prompt and the main findings-selection prompt.
// Empty question = interactive REPL; non-empty = one-shot.
func runReviewChat(worktree *ReviewWorktree, question string, out io.Writer) {
	if worktree == nil {
		fmt.Fprintln(out, "  chat/ask unavailable: review worktree has been torn down")
		return
	}
	fmt.Fprintln(out)
	if question == "" {
		fmt.Fprintln(out, "── chat with the lead reviewer (review-wide) — type /exit or Ctrl-D when done ──")
	} else {
		fmt.Fprintln(out, "── asking the lead reviewer (review-wide) ──")
	}
	fmt.Fprintln(out)
	if err := ChatAboutReview(worktree, question); err != nil {
		fmt.Fprintf(out, "  chat/ask failed: %v\n", err)
	}
	fmt.Fprintln(out, "\n── back to selection ──")
}

// runBareReview drives the bare-review flow: the user wanted to
// approve / comment / request-changes WITHOUT picking any findings to
// post as inline comments. APPROVE allows an empty body; COMMENT and
// REQUEST_CHANGES require non-empty bodies per GitHub's API, so we
// re-prompt if the user skips.
func runBareReview(event Event, scanner *bufio.Scanner, out io.Writer) (*Selection, error) {
	switch event {
	case EventApprove:
		fmt.Fprintln(out, "\nBare APPROVE — no inline comments, optional approval comment.")
	case EventRequestChanges:
		fmt.Fprintln(out, "\nBare REQUEST_CHANGES — no inline comments. GitHub requires a comment explaining what to change.")
	case EventComment:
		fmt.Fprintln(out, "\nBare COMMENT — no inline comments. GitHub requires a body.")
	}

	for {
		global := promptGlobalComment(scanner, out)
		if event != EventApprove && strings.TrimSpace(global) == "" {
			fmt.Fprintln(out, "  GitHub rejects an empty body on this event — please write a short message.")
			continue
		}
		sel := &Selection{Event: event, GlobalComment: global}
		if !confirmPost(sel, scanner, out) {
			return &Selection{}, nil
		}
		return sel, nil
	}
}

// confirmPost renders the exact batched review that would be sent to GitHub
// and asks for explicit y/N. Default is no.
func confirmPost(sel *Selection, scanner *bufio.Scanner, out io.Writer) bool {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "─── Review to be posted ───")
	fmt.Fprintf(out, "Event: %s\n", sel.Event)
	if strings.TrimSpace(sel.GlobalComment) != "" {
		fmt.Fprintln(out, "Global comment:")
		for _, line := range strings.Split(sel.GlobalComment, "\n") {
			fmt.Fprintf(out, "  %s\n", line)
		}
	} else {
		fmt.Fprintln(out, "Global comment: (none — body will be a stub)")
	}
	fmt.Fprintf(out, "Addressing replies: %d\n", len(sel.Addressing))
	for i, a := range sel.Addressing {
		prior, _ := sel.PriorByRef[a.Ref]
		anchor := fmt.Sprintf("%s:%d", prior.File, prior.Line)
		fmt.Fprintf(out, "  [%d] reply to %s on %s — %s (agent: %s)\n", i+1, a.Ref, anchor, a.Status, a.Agent)
	}
	fmt.Fprintf(out, "Inline comments: %d\n", len(sel.Findings))
	for i, f := range sel.Findings {
		anchor := fmt.Sprintf("%s:%d", f.File, f.Line)
		if f.EndLine > f.Line {
			anchor = fmt.Sprintf("%s:%d-%d", f.File, f.Line, f.EndLine)
		}
		fmt.Fprintf(out, "  [%d] %s  (%s · %s · agent:%s)\n", i+1, anchor, f.Severity, f.Category, f.Agent)
		fmt.Fprintf(out, "      %s\n", f.Title)
	}
	fmt.Fprintln(out, "───────────────────────────")

	for {
		fmt.Fprint(out, "Post this review to GitHub? [y/N]: ")
		if !scanner.Scan() {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "y", "yes":
			return true
		case "", "n", "no":
			return false
		default:
			fmt.Fprintln(out, "  expected y or n")
		}
	}
}

// ParseSelection accepts "1,3,5-7" / "all" / "none" / "" and returns the
// 1-based indices (sorted, deduped). max is the inclusive upper bound; any
// index outside [1, max] is an error.
func ParseSelection(input string, max int) ([]int, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" || input == "none" {
		return nil, nil
	}
	if input == "all" {
		out := make([]int, max)
		for i := range out {
			out[i] = i + 1
		}
		return out, nil
	}

	seen := map[int]struct{}{}
	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			ab := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(strings.TrimSpace(ab[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(ab[1]))
			if err1 != nil || err2 != nil || a > b {
				return nil, fmt.Errorf("bad range %q", part)
			}
			for i := a; i <= b; i++ {
				if i < 1 || i > max {
					return nil, fmt.Errorf("index %d out of range (1..%d)", i, max)
				}
				seen[i] = struct{}{}
			}
		} else {
			i, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("bad index %q", part)
			}
			if i < 1 || i > max {
				return nil, fmt.Errorf("index %d out of range (1..%d)", i, max)
			}
			seen[i] = struct{}{}
		}
	}

	out := make([]int, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	// Sort for determinism — small slice, simple insertion sort.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// EditBody opens the user's $EDITOR (or vi as fallback) with `initial` as the
// buffer contents and returns the saved result. The temp file's basename has a
// .md extension so editors enable markdown niceties.
func EditBody(initial string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	dir, err := os.MkdirTemp("", "ox-review-edit-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	tmpPath := filepath.Join(dir, "finding.md")
	if err := os.WriteFile(tmpPath, []byte(initial), 0o600); err != nil {
		return "", err
	}

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor %s: %w", editor, err)
	}

	b, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}

func promptEvent(scanner *bufio.Scanner, out io.Writer) Event {
	for {
		fmt.Fprint(out, "Post as: [c]omment, [r]equest-changes, [a]pprove (default c): ")
		if !scanner.Scan() {
			return EventComment
		}
		switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
		case "", "c", "comment":
			return EventComment
		case "r", "request-changes", "request_changes", "rc":
			return EventRequestChanges
		case "a", "approve":
			return EventApprove
		default:
			fmt.Fprintln(out, "  expected one of c / r / a")
		}
	}
}

func promptGlobalComment(scanner *bufio.Scanner, out io.Writer) string {
	fmt.Fprintln(out, "Optional global comment (cross-cutting commentary that can't be anchored).")
	fmt.Fprintln(out, "Enter lines; finish with a single '.' on its own line, or empty line to skip.")
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "." {
			break
		}
		if line == "" && len(lines) == 0 {
			return ""
		}
		lines = append(lines, line)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
