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
