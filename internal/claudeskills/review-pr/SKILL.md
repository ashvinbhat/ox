---
name: review-pr
description: Review a GitHub PR for Ashvin against the whole codebase at the PR head, not just the diff. On a re-review, grade every open finding against the changes since his last round, then review only the new code. Use when he asks to review or re-review a PR (a URL, or "review Neeraj's PR"). Nothing reaches GitHub until he approves the exact text.
metadata:
  owner: ox
---

# Review a PR

Ashvin reviews only what someone asked him for. Every review reads the code
around the change: a diff hides the call site nobody updated and the parallel
path nobody touched.

## 1. Start

```bash
ox review start <pr-url>
```

Add `--json` if you need to parse it. It prints:

- `checkout`: the full repo at the PR head. Read code here.
- `diff`: the PR diff.
- `asked by`: who asked and the link to their message.
- On a re-review: the earlier rounds, `interdiff` (changes since his last
  reviewed head, flagged when the branch was rebased or force-pushed), and every
  `open` finding (F1, F2, …) not yet addressed.

On his own PR, GitHub allows COMMENT only.

Run `gh pr checks <n> --repo <owner/repo>`. For a red required check, read
the failed job's log and name the cause (a test, the compile, a Sonar gate) in
the verdict. Never re-run CI.

## 2. Understand why before judging code

Read the PR title and body and the ask message. If the body cites a ticket
(CB-XXXXX) and the reason for the change is still unclear, read the ticket
through the Notion connector. Write the intent in two sentences. If you
can't, that is the first finding.

Then ask whether the change does what it is for, not only whether the code is
right. A PR that collects logs or metrics must collect what the question needs:
"which endpoints are not idempotent" needs the repeat's own status, not just a
count of repeats. A gap between the goal and what the code can deliver is an
issue, even when every line is correct.

## 3. Read the diff, then the codebase around it

Read the whole diff yourself. Then, in the checkout, look for what the diff
does not show. For each changed function, type, field, enum value, config key,
endpoint or event:

1. **Callers and readers.** Find every reference (grep or LSP). Do they still
   hold the contract: nullability, ordering, units, errors, defaults?
2. **Siblings.** Check other implementations of the interface, other cases of
   the switch or enum, and the parallel path (LinkedIn vs Google, DM1 vs DM2,
   email vs ads, sync vs async). Did they need the same change?
3. **Consumers outside this repo.** Check the FE generated client, the
   api-automation generated client, python-backend, Temporal workflow and
   activity signatures (replay safety), stored Mongo documents (is a migration
   needed?), and feature flags. Read any counterpart PR the body links (the FE
   change that sends the new header): its values bound what this side accepts.
4. **Duplication.** Does an existing helper already do this?
5. **Tests.** Do they assert the behaviour or just run it? Is each behaviour
   tested at the lowest layer that catches it? Is every deleted or disabled
   test justified? For code behind a guard (a filter's `shouldNotFilter`, a
   flag, a null check that skips), find the test that turns it on through the
   real wiring. If every test mocks that wiring, the way it silently does
   nothing in production is untested.
6. **Conventions.** Apply the repo's CLAUDE.md or AGENTS.md, the repo's own
   skills for the area (`.agents/skills/` in backend and frontend), and
   Ashvin's memory rules for that stack. A repo skill that the change makes
   stale (a filter order, a recipe, a list of steps) is a finding.
7. **Security.** Who can send this input: any logged-in user, a service key,
   anonymous? What reaches storage, logs and metric tags verbatim? Is it scoped
   per tenant? Is metric cardinality bounded?

For a large PR, one Explore subagent may sweep callers and siblings. Read the
diff yourself and don't fan out reviewer panels.

## 4. Re-review (when earlier rounds exist)

1. For each open finding, read the interdiff (the full diff if there is none)
   and give a verdict: addressed, partial or ignored, with `file:line` evidence.
2. Review only the new hunks, using step 3.
3. If the branch was rebased, the interdiff can mislead. Run
   `git range-diff <old-base>..<old-head> <base>..<head>` in the checkout.

## 5. Validate every finding

Open the code and confirm each finding before you show it. Trace the callers.
Drop anything you can't point to in the code, and list what you dropped with a
one-line reason each.

Check the factual claims in the PR body and the new comments too: a timeout
in another repo, "nothing maps this to Redshift", "the index must exist first".
A wrong reason in a comment is a finding; a confirmed one goes in the dropped
list, so he sees it was checked.

## 6. Show Ashvin, in chat

1. **Verdict:** approve, comment or request changes, with one line why.
2. **Intent:** two sentences.
3. **Findings**, blocker → issue → suggest → nit. Each gets `file:line`,
   severity, a one-line claim, the evidence (name the file outside the diff
   that proves it), and the fix. Use a suggestion block for a line change.
4. **Re-review:** a table of open findings with verdict and evidence.
5. **New to you:** up to 3 concepts from this PR's domain he likely doesn't
   know, 3 lines each. Skip the section when there are none.
6. **Reading order:** files in the order you'd explain them (contract, then
   implementation, then tests).
7. **Walkthrough:** when the change has a flow (a request path, a workflow, a
   state machine), draw it inline: a flow diagram of the path, then a
   click-through of the cases it handles, each showing what the code sees and
   what it records. Mark where a finding bites. Skip it for a refactor or a
   config change.

## 7. Post only what he approves

Draft the GitHub text: specific, actionable, in his voice, with no mention of
ox, yoke, Claude or AI. Wait for his OK on the exact findings and the event.
Then write `round.json` in the review's work dir (the folder holding the diff)
and post:

```bash
ox review post <pr-url> --file <work-dir>/round.json --confirm
```

```json
{"event": "COMMENT", "body": "…",
 "findings": [{"file": "path/from/root", "line": 42, "severity": "issue", "category": "correctness", "title": "…", "body": "…"}],
 "addressing": [{"ref": "F2", "status": "addressed", "note": "…"}]}
```

- Inline anchors must be lines in the diff. Put points outside the diff, and
  the CI note, in `body`. A finding that spans files goes on one line and names
  the others.
- A `suggestion` block replaces only the anchored line. A fix that touches
  other lines or files goes in a plain code block.
- If he posts the review himself or gives it in person, save the round with
  `ox review record <pr-url> --file <work-dir>/round.json`.
- Every round gets posted or recorded, so the next re-review knows what is
  still open.
- When a re-review shows everything addressed, offer to resolve his threads.

## 8. Improve this skill

This skill learns from each review. After the round, look for:

- a correction from Ashvin, or a finding he cut or reworded;
- something he asked for that the skill didn't produce;
- a class of finding that no step in 3 prompted, which you found anyway.

Propose each as a one-line change to this file, in ox at
`internal/claudeskills/review-pr/SKILL.md`. On his yes, make it and ship it as
an ox PR with a CHANGELOG entry. Keep it general: a rule for every review, not
a note about this PR.
