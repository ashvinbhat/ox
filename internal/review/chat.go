package review

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ChatAboutFinding launches an interactive claude session pinned to a
// specific finding. The session inherits the originating agent's
// instructions and the worktree as cwd, so claude can grep/read files in
// the PR head while defending or refining its verdict.
//
// userQuestion is optional. If empty, the session opens in fully
// interactive mode (user types the first message). If non-empty, claude
// runs in print mode (-p) and exits after one answer.
func ChatAboutFinding(worktree *ReviewWorktree, finding Finding, userQuestion string) error {
	spec, ok := lookupAgentSpec(finding.Agent)
	if !ok {
		// Unknown agent (manually-edited or dedup-merged finding) — fall back
		// to a neutral reviewer persona.
		spec = AgentSpec{
			Name:         finding.Agent,
			Focus:        "code review discussion",
			Instructions: "Discuss the code review finding the user raises. Be precise, cite code from the worktree, do not hallucinate file/line references.",
		}
	}

	systemPrompt := buildChatPrompt(spec, finding)

	args := []string{
		"--dangerously-skip-permissions",
		"--append-system-prompt", systemPrompt,
	}
	if userQuestion != "" {
		// One-shot: claude runs the question and exits.
		args = append(args, "-p", userQuestion)
	}
	// Else: claude opens interactive REPL; user owns the conversation.

	cmd := exec.Command("claude", args...)
	cmd.Dir = worktree.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ChatAboutReview launches an interactive (or one-shot) claude session for
// GENERAL questions about the whole review — not pinned to a single finding.
// Use this for "what do you think of this PR overall", "which of these is
// the highest-priority", "are these findings consistent with each other",
// etc.
//
// userQuestion is optional. Empty → interactive REPL. Non-empty → one-shot
// with -p (claude prints the answer and exits).
//
// The agent runs with the review worktree as cwd, so it can read
// .ox/review/REVIEW.md (full context — PR meta + files + diff + prior
// findings + addressing diff) and .ox/review/findings/*.json (the per-agent
// outputs) directly.
func ChatAboutReview(worktree *ReviewWorktree, userQuestion string) error {
	systemPrompt := buildReviewChatPrompt()

	args := []string{
		"--dangerously-skip-permissions",
		"--append-system-prompt", systemPrompt,
	}
	if userQuestion != "" {
		args = append(args, "-p", userQuestion)
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = worktree.Path
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildReviewChatPrompt() string {
	return `You are the lead reviewer for a code review session that has already
been run. The user wants to discuss the review holistically — not a
single finding.

# Context available to you
- The PR is checked out at the current working directory.
- ".ox/review/REVIEW.md" carries the full review context: PR metadata,
  files changed, the unified diff, repo conventions, prior findings
  (on follow-up reviews), and the addressing diff.
- ".ox/review/findings/<agent>.json" carries each reviewer agent's raw
  output as a JSON object with {findings: [...], addressing: [...]}
  arrays. Agents are: correctness, security, design, test.
- ".ox/review/logs/<agent>.log" carries each agent's chat-mode log
  (their internal reasoning during the run).

Read these as needed. Open files in the worktree to ground your answers.

# How to behave
- Answer the user's questions about the review as a whole. Cite specific
  findings by ref (e.g. "F4") or by file:line when relevant.
- Be willing to refine the team's verdicts when the user surfaces
  evidence that was missed. Be willing to defend them when the pushback
  is weak.
- If asked for an overall recommendation (approve / comment /
  request-changes), give it directly with the reasoning.
- Keep responses tight. One or two short paragraphs is usually enough.

# Hard constraints
- DO NOT propose new findings — those would require a separate review run.
- DO NOT modify any source files in the worktree.
- DO NOT post to GitHub. Your only output is this chat.

The user's first message follows.`
}

// lookupAgentSpec returns the AgentSpec whose Name matches `name`, with the
// dedup-merged "a+b" form fallback resolved to the first underlying agent.
func lookupAgentSpec(name string) (AgentSpec, bool) {
	// Dedupe may have combined agent attributions like "security+correctness".
	primary := name
	if i := strings.Index(name, "+"); i >= 0 {
		primary = name[:i]
	}
	for _, spec := range AllAgents {
		if spec.Name == primary {
			return spec, true
		}
	}
	return AgentSpec{}, false
}

func buildChatPrompt(spec AgentSpec, finding Finding) string {
	anchor := fmt.Sprintf("%s:%d", finding.File, finding.Line)
	if finding.EndLine > finding.Line {
		anchor = fmt.Sprintf("%s:%d-%d", finding.File, finding.Line, finding.EndLine)
	}

	return fmt.Sprintf(`You are continuing a code review discussion as the %q reviewer.

# Original review focus
%s

# The finding under discussion
**[%s · %s]** %s

Anchor: %s
Agent attribution on the finding: %s

Body of the finding (as originally raised):
%s

# How to behave in this discussion
- The user is asking questions about THIS specific finding. Stay focused on it.
- The working directory is the PR head checkout. You may read source files in the
  worktree, run %s if useful, and reference exact file:line locations. Do not
  hallucinate paths or line numbers — open the file if you are unsure.
- Be willing to refine, sharpen, or retract the verdict if the user surfaces
  evidence you missed (e.g. "this is actually handled by X" or "Y already covers
  this"). Say so plainly when you change your mind.
- Be willing to defend the verdict if the pushback is weak. Reviewer reasoning
  should not collapse on the first objection — explain WHY the original concern
  stands, with specifics.
- Keep responses tight. One or two short paragraphs is usually enough.
- Do NOT propose new findings or wander into adjacent issues — those are out of
  scope for this discussion.
- Do NOT modify any source files in the worktree.
- Do NOT post anything to GitHub. Your only output is this chat.

The user's first message follows.`,
		spec.Name,
		strings.TrimSpace(spec.Instructions),
		finding.Severity,
		finding.Category,
		finding.Title,
		anchor,
		finding.Agent,
		strings.TrimSpace(finding.Body),
		"`grep` / `find` / file reads",
	)
}
