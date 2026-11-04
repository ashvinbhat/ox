---
name: tidy
description: Tidy Ashvin's Claude Code desktop threads — archive finished ones, rename threads that share a title, file threads into one sidebar group per track, and clear what the ox board shows (questions that repeat another thread's, stuck threads that actually finished, answers he gives here passed on) — proposed in one block and applied only after he approves. Use when he says "tidy", "clean up my threads", "clean up the board", "/tidy", or asks about the "can be archived" note in the thread brief.
metadata:
  owner: ox
---

# Tidy

Keep the sidebar and the board small enough to scan. Propose everything in
one block, apply only what he approves, and never touch a thread that still
has work in it.

"Clean up the board" is a smaller run: propose only the board items (step 2,
last part). "Clean up my threads" and "tidy" propose all of it.

## 1. Read the state

```bash
ox now --json
ox asks --json --offline
grep -E "name:|short:" ~/.ox/tracks.yaml
```

Then the app's view, with the session and sidebar tools (load them with
ToolSearch if they're deferred): `list_sessions` (limit 50) for each thread's
`group`, `pinned`, `prState` and `startedBy`, and `list_groups`.

## 2. Build the proposal

**Archive** a thread when `ox now` marks it `archivable` (done, idle 48h+),
or when all its PRs are merged or closed and it has been idle 24h+. Never
archive:

- this thread,
- a running thread,
- a parked thread (`parked` in `ox now`): he left it on purpose,
- a thread with an open PR,
- a thread whose question is in the asks queue, unless he picks archive for
  that question below.

**Rename** threads that share a title. Keep the title on the one the others
were started from (or the oldest) and give each other one
`<title> · <what it did>`, with 2–5 words from its recap, branch or PR
("Durable chat turns · FE Redis transport").

**Group by track.** One sidebar group per track in `tracks.yaml`, named by
the track's `name`; create only the missing ones. File each thread into its
track's group (the `track` in `ox now --json`). Leave a thread alone if it
has no track, or if it sits in a group he made that isn't a track's group.

**Board items.** What the board lists, one line each with what you'd do:

- A thread's question (an ask of kind `question`). Quote it and offer:
  answer it here, drop it (`ox asks drop <id>`: hidden until that thread asks
  something new), or archive the thread. Recommend drop when the question
  only points at another thread's question, or a later message settled it.
  Say "stale" for one `ox asks` marks stale.
- A PR ready to merge (kind `merge`). Merging is his: list it, nothing more.
- A thread `ox now` marks `stuck`. Read its last messages (`list_events`,
  limit 6). If its last turn ended with a full answer, it's done, not stuck:
  recommend archive. If it stopped mid-work, offer to send it "carry on" or
  archive it.

Show it as one numbered block, one line each, then ask once which to apply:

```
1. Archive "Durable agents exploration" (done, idle 13d)
2. Archive "Requirements review and scope" (done, idle 6d)
3. Rename "Durable chat turns: …" (side session -2) → "Durable chat turns · Valkey infra"
4. Group: create "Durable agents"; file 7 threads
5. "MigrationRunWorkflow failures": "start the Canva landing redesign? (other thread)" repeats 6: drop
6. "Landing migration (SEG022)": "commission a redesign of the union-PGC fix?": answer, drop or archive?
7. "Side chat" shows stuck, but its last turn finished: archive
```

He can answer a question in his reply ("6: no, hold it"). Those words are
his answer to that thread.

## 3. Apply what he approved

Use `archive_session`, `set_session_title`, `create_group`,
`move_sessions` and `ox asks drop`. Pass an answer to its thread with
`SendMessage` (`to` is the thread's `local_…` id), in his words, unchanged.
Then report in one line: archived, renamed, filed, dropped, answered.

## Rules

- One approval per run. Nothing changes before it.
- Never delete a thread. Archive is the most this skill does, and it can be
  undone.
- Don't rename or regroup threads he named or filed himself unless they're
  in the proposal he approved.
- Pass his answers on as he wrote them. Don't reword them or add your own.
