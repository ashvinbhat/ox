# Work list

One list of everything Ashvin's work is made of, so threads can be archived
and nothing is lost. yoke holds the list, ox keeps most of it from what it
already sees (threads, PRs, merges), threads add what takes judgment, and the
board shows what waits for his go.

## Words

- **Ticket**: an EPD Tasks page in Notion assigned to Ashvin, in the current
  sprint (the Sprints page whose Sprint status is Current). The team's unit
  of work. It lives in Notion; yoke keeps a linked copy.
- **Work**: one thing Ashvin asked a thread to do that leaves something
  lasting. It ends in PRs, or in an outcome when there's nothing to ship.
- **Lasting**: the thread leaves something behind: a PR, a finding, a design,
  an investigation's answer, or a change outside code (a send, a delete, a
  config change). A lookup answered in chat that changes nothing isn't
  lasting and gets no task. Unsure means lasting: a task is cheap, lost work
  isn't.
- **Finding**: something a thread noticed and didn't do: a bug, a follow-up, a
  cleanup, a question for later, or a later step the thread promised. It
  waits for his go.
- **Go**: Ashvin saying a finding should be done. Until then it is only
  recorded, unless it is small.
- **Small**: a finding in code an open PR of this thread already touches, a
  few lines, no behaviour anyone relies on changes, and no design choice: a
  typo, dead code, a wrong comment, a missing assertion. The thread fixes it
  in that PR without waiting for his go.
- **Key**: what makes a task findable and one of a kind: `CB-<n>` for a
  ticket, `pr:<repo>-<n>` for each PR a work holds, `thread:<local id>` for
  each thread that worked on it. Adding a task under a key that already
  exists reopens that task instead, closed or not, so its history stays in
  one place.
- **Found from**: the link from a finding to the work it came from. It
  doesn't block anything; it answers "what came out of this work?".
- **Track**: one of his workstreams in `tracks.yaml`. Every task carries one.
- **Thread**: a desktop session. It does work, records findings, and is
  archived once its work is done.

## What yoke holds

| Kind | Parent | Keys | Status | Tags | Holds | Notion |
|---|---|---|---|---|---|---|
| Ticket | none | `CB-<n>` | from Notion: `pending`, `in_progress`, `blocked`, `done` | `ticket`, track | the Notion link | linked; status pulled, never pushed |
| Work | its ticket, or none | `thread:<id>` for each thread, `pr:<repo>-<n>` for each PR | `in_progress` → `done` or `dropped` | `work`, track, repo | his ask in his words, PRs as notes, an outcome line | local only |
| Finding | the ticket it came from, or none | none | `pending` (waiting for go) → `active` (go) → `in_progress` → `done` or `dropped`; a small one goes straight to `done` | `finding`, track | what, where, why it matters, how to pick it up, found from its work | local only |

yoke pushes only tasks linked to a Notion page, so work and findings never
reach the team's board.

## Who does what

ox does the bookkeeping it can see for itself. A thread does only what takes
judgment: whether work is lasting, what it found, what it promised.

| When | What happens | Done by |
|---|---|---|
| Every hour | Tickets assigned to Ashvin in the current sprint come into yoke; every linked ticket still open picks up its Notion status, even after its sprint ends | ox's hook runs a new `yoke sync` |
| A thread opens a PR | Work found by the PR's key, else by the thread's, else created under the ticket its CB id names (from the PR title or branch); the PR's key and note added | ox, at each PR refresh |
| A thread does lasting work with no PR | Work added under the thread's key (found and reopened if it exists) | the thread |
| A thread notices something small | Fixed in the PR that touches it; finding recorded as `done` with that PR | the thread |
| A thread notices something it won't do now | Finding, `pending`, found from its work | the thread, in the same reply |
| A thread promises a later step ("I'll check X after the merge") | Finding, `pending`, found from its work, with the date if it's timed | the thread, in the same reply |
| A later thread picks up the same PR or ticket | The work is found by its key and reopened; the new thread's key added | the thread, or ox when it's a PR |
| A PR merges | Work `done` once all its PRs merged; outcome from the PR titles | ox, at each PR refresh |
| Work with no PR ends | Work `done`, with a one-line outcome | the thread, on its last turn |
| Ashvin says go | Finding `active`; started in that thread or picked up by another | the thread he says it in |
| Ashvin says drop | Finding `dropped` | the thread he says it in |
| A ticket is done in Notion | yoke's ticket `done`; open findings under it stay for him to decide | the sync |

Not a promise, so no finding: a step done in this reply, a step waiting on
his answer (that's the thread's question), a step a PR, a ticket or ox
already owns, a rule being described.

## The board

- The threads that need him, as now.
- Findings waiting for his go, one line per ticket:
  "CB-18304 Video transcription · 3 findings". The × hides the line until a
  new finding arrives under that ticket.

## Archiving

A daily routine archives a thread when all of these hold, then posts what it
archived:

- its last turn finished, and no question is waiting on him,
- it isn't parked,
- it has no open PR,
- all its work is `done` or `dropped`,
- its worktree has no uncommitted or unpushed changes,
- it has been quiet for 24 hours.

Pending findings don't keep a thread open: they live in yoke.

## Context for a new thread

The brief gains one line: "Tickets: 5 open · Findings: 7 waiting for your go".
A thread whose PR or thread key already has work is told its work in the
brief. Once a thread knows its ticket, it reads `yoke context <ticket>`:
earlier work and outcomes, findings, notes.

## Build

All four steps shipped on 2026-11-06.

1. yoke: keys (adding under an existing key reopens), the found-from link,
   and `yoke sync` for current-sprint tickets (yoke#2).
2. ox: work for every thread with a PR (found or created by key, closed on
   merge), the brief lines, and two always-loaded rules: lasting work gets a
   task; a promise or a noticed-not-done step is a finding in the same reply
   (ox#41). A PR belongs to the thread that opened it, else the oldest one
   holding it, so one shared PR doesn't join two threads' work.
3. Board: the findings line.
4. The daily archive routine: `ox work archivable` holds the rules, the
   `daily-archive` skill archives through the app, a scheduled task runs it
   each morning. Only a thread's own worktree is checked for changes: a
   shared checkout's changes can't be told apart by thread.

## Not in scope

- Writing anything back to Notion. Ticket status stays the team's.
- Picking work up from Slack's To-dos list. The Slack connector can't read
  it, and scraping Slack's web page would break easily (2026-11-05).
- A model reading transcripts to judge whether a thread is done.
- A hook that blocks a thread with no task. Jarvis's own rollout of that
  hook stops at logging; here ox keeps the task, so there's nothing to police.
- Required label sets on every task, and a master label list. Keys and the
  track are enough.
- Semantic search before adding a task. Keys cover tickets, PRs and threads;
  revisit if keyless duplicates show up.

## Decided (2026-11-05)

1. Tickets: the current sprint only.
2. Small findings: a thread fixes them without waiting for his go.
3. Archiving: the routine archives on its own, then lists what it archived.
4. Board: one line per ticket with findings waiting.
5. Findings without a ticket: top-level tasks with the track (default).
6. Notion: never written to; starting work doesn't move a ticket (default).

## Added (2026-11-06, from how Jarvis runs beads)

7. Keys: a task is found by its ticket, PR or thread; adding under a key
   that exists reopens that task.
8. ox keeps the work of threads with PRs (create, attach PRs, close on
   merge); a thread adds work only for lasting work without a PR.
9. A promised later step, like anything noticed and not done, is a finding
   in the same reply, found from its work.
10. No enforcing hooks: the two rules sit in CLAUDE.md, and ox does the rest.
