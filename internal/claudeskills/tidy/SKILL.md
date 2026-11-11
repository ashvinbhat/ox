---
name: tidy
description: Tidy Ashvin's Claude Code desktop threads — archive finished ones, rename threads that share a title, file threads into one sidebar group per track, and clear what the ox board shows (questions that repeat another thread's, stuck threads that actually finished, answers he gives here passed on) — applied, then reported in one block with how to undo each. Use when he says "tidy", "clean up my threads", "clean up the board", "/tidy", or asks about the "can be archived" note in the thread brief.
metadata:
  owner: ox
---

# Tidy

Keep the sidebar and the board small enough to scan. Do what the rules below
call for, then report it in one block with how to undo each. Never touch a
thread that still has work in it. A question a thread is waiting on him for
is his: list it, don't answer it.

"Clean up the board" is a smaller run: only the board items (step 2, last
part). "Clean up my threads" and "tidy" do all of it.

## 1. Read the state

```bash
ox now --json
ox asks --json --offline
grep -E "name:|short:" ~/.ox/tracks.yaml
```

Then the app's view, with the session and sidebar tools (load them with
ToolSearch if they're deferred): `list_sessions` (limit 50) for each thread's
`group`, `pinned`, `prState` and `startedBy`, and `list_groups`.

## 2. Work out what to do

**Archive** a thread when `ox now` marks it `archivable` (done, idle 48h+),
or when all its PRs are merged or closed and it has been idle 24h+. Never
archive:

- this thread,
- a running thread,
- a parked thread (`parked` in `ox now`): he left it on purpose,
- a thread with an open PR,
- a thread whose question is in the asks queue, unless the question is
  settled (below).

**Rename** threads that share a title. Keep the title on the one the others
were started from (or the oldest) and give each other one
`<title> · <what it did>`, with 2–5 words from its recap, branch or PR
("Durable chat turns · FE Redis transport").

**Group by track.** One sidebar group per track in `tracks.yaml`, named by
the track's `name`; create only the missing ones. File each thread into its
track's group (the `track` in `ox now --json`). Leave a thread alone if it
has no track, or if it sits in a group he made that isn't a track's group.

**Board items.** What the board lists:

- A thread's question (an ask of kind `question`). Dismiss it (`ox asks drop
  <id>`) when it only points at another thread's question, or a later
  message settled it. One marked `settled` ("ox#35 merged since") was asked
  before its PR merged: read the question; if the merge answered it (a
  merge, reinstall or go-ahead ask), archive the thread when it has nothing
  else open, else dismiss it. Any other question is his: list it under
  "Still yours".
- A PR ready to merge (kind `merge`). Merging is his: list it under "Still
  yours".
- A thread `ox now` marks `stuck`. Read its last messages (`list_events`,
  limit 6). If its last turn ended with a full answer, it's done: archive it.
  If it stopped mid-work, list it under "Still yours".

## 3. Apply, then report

Use `archive_session`, `set_session_title`, `create_group`,
`move_sessions` and `ox asks drop`. Then report one block:

```
Done:
1. Archived "Durable agents exploration" (done, idle 13d)
2. Renamed "Durable chat turns: …" (side session -2) → "Durable chat turns · Valkey infra"
3. Filed 7 threads into "Durable agents" (new group)
4. Dismissed "MigrationRunWorkflow failures": its question repeats Landing migration's
Undo: unarchive or rename in the sidebar; a dismissed question comes back when its thread asks again.

Still yours:
- Send · Landing migration (SEG022): commission a redesign of the union-PGC fix?
- "Side chat" stopped mid-work: say "carry on" and I'll send it on
```

He can answer an item in his reply ("Landing: no, hold it"). Pass it to that
thread with `SendMessage` (`to` is the thread's `local_…` id), in his words,
unchanged.

## Rules

- Never delete a thread. Archive is the most this skill does, and it can be
  undone.
- Don't rename or regroup threads he named or filed himself.
- Pass his answers on as he wrote them. Don't reword them or add your own.
