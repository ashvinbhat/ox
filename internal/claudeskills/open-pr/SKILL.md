---
name: open-pr
description: Open a PR for the change in this thread. Format, start the targeted tests in the background, open the PR as a draft while they run, and mark it ready for review on its own once they pass locally. Use when Ashvin says "open the PR", "raise a PR", "put it up", "ship it", or a change built in this thread is ready to go out.
metadata:
  owner: ox
---

# Open a PR

Ashvin wants the PR up early so he can start reading, and out of draft only
once the tests this change wrote or touched pass locally. On CBX1 repos CI
runs off the `ready-for-review` label and skips drafts, so it starts when the
PR goes ready and gets the label.

## 1. Title

`CB-XXXXX: <what changes, in plain words>`. Take the ticket id from the thread
or ask him. Never guess a number.

- Team repos (backend, frontend, api-automation, …) always take a real CB
  ticket: their CI checks it in Notion.
- `CB-0000` only on ox and yoke PRs. ox PRs (ashvinbhat/ox) also get an entry
  under `[Unreleased]` in CHANGELOG.md.

## 2. Check, format, then start the tests

Check the diff against his rules for the stack:

- **Backend:** no inline FQCNs; no `while (true)`, `for (;;)` or
  deadline-only loops (bound each by a count from the data); no new comments
  and no CB ids in code.
- **Frontend:** `npx i18n-check -s en --locales messages/` passes, so every
  key is in en, es, fr and de.
- **api-automation:** Given/When/Then the repo's way, at most 2 test methods,
  exact assertions with a control where a check could pass vacuously.

Format, the repo's way: backend
`./mvnw spotless:apply -pl <touched modules>`, ox `gofmt`. Never reformat
files the change didn't touch.

Then start the targeted tests in the background (`run_in_background`), log
to a file in the scratchpad: the classes this change added or changed, and
the ones that construct what it changed. Backend:

```bash
./mvnw -pl <module> -am -Dsurefire.failIfNoSpecifiedTests=false \
  -Dtest='<ClassA>,<ClassB>' test > "$LOG" 2>&1
```

Typecheck, lint and the pre-push hook run in the background with the tests.
Push once you're ~80% sure; don't hold the push or the turn on any of them.

One build per checkout: no formatter or second build while it compiles.

## 3. Open the draft while they run

- Stage by path, check `git diff --cached --name-status`, commit, push.
- `gh pr create --draft --base main --title … --body-file …`, with no
  labels. `ready-for-review` goes on only when the PR goes ready.
- Body: the problem first, then what changes, what's not covered, and
  testing ("local run in progress"). A new or changed Temporal workflow
  names the 2–3 house workflows it mirrors.
- Nothing internal on GitHub: no yoke or ox ids, task numbers, thread
  references or AI-authorship trailers in commits, the title, the body or
  comments.
- Bind it to the thread: `get_status`, then `bind_pr` if it isn't bound.

## 4. When the tests finish

Read the log (`Tests run:`, `BUILD SUCCESS`), not the exit code alone.

- **Green:** update the body's testing section with the classes and counts,
  then `gh pr ready <number>` and, on CBX1 repos,
  `gh pr edit <number> --add-label ready-for-review` (CI and Auto-fix key off
  it). Tell him in one line.
- **Red from this change:** fix, push, run again. It stays a draft until a
  run is green.
- **Red from something else** (it fails before the changed code runs, or in
  code the change didn't touch): name the failure in the body's testing
  section and go ready as on green.

## 5. After it's up

- After every push, refresh the title and body so they describe the branch
  as it is now, including what was reverted and what's left for follow-ups.
- CI red from this change: reproduce it locally and fix it. Never trigger or
  re-run CI.
- A live run, or screenshots on the PR, waits until DARS has approved with no
  open comments and the code is final.

## Rules

- Never mark ready on a partial run, a skipped run or an assumption.
- Never label a draft.
- The wider suite (whole module, full reactor) is his to start: name it in
  the report, don't run it or ask about it.
- One logical change per PR. Merging is his.
