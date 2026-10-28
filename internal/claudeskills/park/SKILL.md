---
name: park
description: Park this thread — Ashvin is leaving it on purpose and coming back later. Writes a resumption note as the thread's last message and records the next step with `ox park`, so the brief and board show the thread as parked, not done, and /tidy leaves it alone. Use when he says "park this", "I'll come back to this", "stopping here for now", "pick this up later", or "/park".
metadata:
  owner: ox
---

# Park

He may come back days from now with none of today in his head. Leave the cue
where he'll look: the end of this thread, and the board.

## 1. Gather the state yourself

Don't ask him. For each repo this thread touched:

```bash
git status --short
git branch --show-current
git log --oneline @{u}.. 2>/dev/null   # unpushed commits
```

Plus the thread's open PRs and their state, and anything still running
(servers, watchers, background jobs): stop what's safe to stop, name the rest.
Log any decision made here that isn't in the journal yet (`ox journal add`).

Leave uncommitted changes where they are and list them. Commit or push only
if he says so.

## 2. Record the next step

```bash
ox park --next "<the first concrete action: a verb plus a file, command or PR>"
```

"Rebase frontend#9680 on main and rerun the server build", not "continue".

## 3. Write the note as your last message

This shape, nothing after it:

```
**Parked · Wed 28 Oct**
- Goal: <one line>
- Done: <what's finished, with PR and commit refs>
- Next: <the step you recorded, concrete enough to start without rereading>
- Open: <decisions waiting on him, or "none">
- State: <branch, uncommitted files, unpushed commits, PRs, anything running>
```

## Coming back

When he writes in a parked thread again, read the note first and start at
Next. The thread stops being parked on its own once it's active again.

## Rules

- Under 12 lines. Facts, not a story of the session.
- Never end with a question: a question marks the thread "needs you" and puts
  it in his queue.
- Don't archive, rename or regroup the thread.
