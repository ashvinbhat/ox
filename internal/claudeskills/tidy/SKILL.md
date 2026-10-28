---
name: tidy
description: Tidy Ashvin's Claude Code desktop threads — archive finished ones, rename threads that share a title, file threads into one sidebar group per track, and ask about stale questions — proposed in one block and applied only after he approves. Use when he says "tidy", "clean up my threads", "/tidy", or asks about the "can be archived" note in the thread brief.
metadata:
  owner: ox
---

# Tidy

Keep the sidebar small enough to scan. Propose everything in one block, apply
only what he approves, and never touch a thread that still has work in it.

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
- a thread whose question is in the asks queue.

**Rename** threads that share a title. Keep the title on the one the others
were started from (or the oldest) and give each other one
`<title> · <what it did>`, with 2–5 words from its recap, branch or PR
("Durable chat turns · FE Redis transport").

**Group by track.** One sidebar group per track in `tracks.yaml`, named by
the track's `name`; create only the missing ones. File each thread into its
track's group (the `track` in `ox now --json`). Leave a thread alone if it
has no track, or if it sits in a group he made that isn't a track's group.

**Stale questions.** For each ask marked `stale` in `ox asks`, quote the
question and offer: answer it in that thread, archive the thread, or drop the
ask (`ox asks drop <id>`).

Show it as one numbered block, one line each, then ask once which to apply:

```
1. Archive "Durable agents exploration" (done, idle 13d)
2. Archive "Requirements review and scope" (done, idle 6d)
3. Rename "Durable chat turns: …" (side session -2) → "Durable chat turns · Valkey infra"
4. Group: create "Durable agents"; file 7 threads
5. Stale (4d) "Is a move to GCP planned?": answer, archive, or drop?
```

## 3. Apply what he approved

Use `archive_session`, `set_session_title`, `create_group` and
`move_sessions`. For a stale question, do what he chose. Then report in one
line: archived, renamed, filed, dropped.

## Rules

- One approval per run. Nothing changes before it.
- Never delete a thread. Archive is the most this skill does, and it can be
  undone.
- Don't rename or regroup threads he named or filed himself unless they're
  in the proposal he approved.
