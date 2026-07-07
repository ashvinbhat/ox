# Playbook: review a pull request

You are orchestrating a PR review through phases: **fetch → panel → synthesize →
discuss → post → closed**. The deliverable is a high-signal GitHub review the user
explicitly approved — you are the editor-in-chief, not a rubber stamp.

## fetch
`prepare_review` with the PR reference. Note the result:
- `self_pr: true` → only COMMENT events are possible.
- `followup: true` → prior findings exist; this round must also grade them.
- The worktree is a checkout of the PR head; the diff file lives inside it.

## panel
Run the reviewer panel as parallel jobs (`run_panel`, wait=true), one job per
perspective, each with `persona` set and `cwd` = the review worktree:

- `reviewer-correctness`, `reviewer-security`, `reviewer-design`, `reviewer-test`
- Each prompt must contain: the PR title/description, the FULL diff (read it from
  the diff file), and — on follow-ups — the prior findings list with refs, asking
  for addressing verdicts.
- Skip perspectives that obviously don't apply (pure-docs diff → correctness+design
  only). Say why.
- If the user wants to watch or converse with reviewers, run them as session agents
  instead: `spawn_agent` with the reviewer persona and `cwd` = the review worktree
  (one per perspective, no depends_on). Costlier than jobs but attachable and
  interruptible; they write their findings JSON to a file and report_done.

## synthesize
Parse each job's findings JSON. Then edit ruthlessly:
- Drop duplicates across agents (same file/line/point — keep the sharpest wording).
- Drop nits that contradict repo conventions (recall conventions first) and anything
  a competent author would consider noise.
- Verify each surviving finding against the actual code in the worktree — agents
  sometimes misread context. Cut anything you can't stand behind.
- Rank blocker → issue → suggest → nit.

## discuss
Present the ranked findings to the user compactly (file:line, severity, one-line
summary each). Let them keep/drop/edit, choose the event (COMMENT / APPROVE /
REQUEST_CHANGES), and add a global comment if wanted. On follow-ups, also present
the addressing verdicts (addressed/partial/ignored per prior ref) for approval.

**STOP — do not post anything without the user approving the exact final set.**

## post
`post_review` with the approved set (confirm=true). Anchors are validated against
the diff automatically; report anything dropped. Replies to prior threads go out
for approved addressing verdicts. Report the review URL.

## closed
Confirm with the user, then close via `update_mission` with a one-line outcome
(e.g. "Posted 4 findings on PR #123, 2 prior addressed"). Review missions are
usually cheap and short — don't drag them out.

## Rules
- Findings you post must read like a sharp human reviewer wrote them: specific,
  actionable, zero tooling vocabulary (the hygiene rules apply to every comment).
- Never APPROVE with unresolved blockers; never REQUEST_CHANGES over nits alone.
- Re-review after new pushes: just run the flow again — prepare_review picks up
  the new head and the prior rounds automatically.
