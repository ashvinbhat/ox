---
name: open-pr
description: Open a PR for the change in this thread. Format, start the targeted tests in the background, open the PR as a draft while they run, and mark it ready for review on its own once they pass locally. Use when Ashvin says "open the PR", "raise a PR", "put it up", "ship it", or a change built in this thread is ready to go out.
metadata:
  owner: ox
---

# Open a PR

Ashvin wants the PR up early so he can start reading, and out of draft only
once the tests this change wrote or touched pass locally. On CBX1 repos the
build skips drafts, so CI starts when the PR goes ready.

## 1. Title

`CB-XXXXX: <what changes, in plain words>`. Take the ticket id from the thread
or ask him; `CB-0000` when the work has no ticket. Never guess a number.
ox PRs (ashvinbhat/ox) take a `feat:` or `fix:` title and an entry under
`[Unreleased]` in CHANGELOG.md.

## 2. Format, then start the tests

Format first, the repo's way: backend
`./mvnw spotless:apply -pl <touched modules>`, ox `gofmt`. Never reformat
files the change didn't touch.

Then start the targeted tests in the background (`run_in_background`), log
to a file in the scratchpad: the classes this change added or changed, and
the ones that construct what it changed. Backend:

```bash
./mvnw -pl <module> -am -Dsurefire.failIfNoSpecifiedTests=false \
  -Dtest='<ClassA>,<ClassB>' test > "$LOG" 2>&1
```

One build per checkout: no formatter or second build while it compiles.

## 3. Open the draft while they run

- Stage by path, check `git diff --cached --name-status`, commit, push.
- `gh pr create --draft --base main --title … --body-file …`, plus
  `--label ready-for-review` on CBX1 repos: CI and Auto-fix key off it,
  drafts included.
- Body: the problem first, then what changes, what's not covered, and
  testing ("local run in progress"). No yoke, ox or thread references.
- Bind it to the thread: `get_status`, then `bind_pr` if it isn't bound.

## 4. When the tests finish

Read the log (`Tests run:`, `BUILD SUCCESS`), not the exit code alone.

- **Green:** update the body's testing section with the classes and counts,
  then `gh pr ready <number>`. Tell him in one line.
- **Red from this change:** fix, push, run again. It stays a draft until a
  run is green.
- **Red from something else:** leave it draft, show him the failure, ask.

## Rules

- Never mark ready on a partial run, a skipped run or an assumption.
- The wider suite (whole module, full reactor) is his call: offer it, run it
  on a yes.
- One logical change per PR. Merging is his.
