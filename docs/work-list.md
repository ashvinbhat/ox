# Work list (draft)

One list of everything Ashvin's work is made of, so threads can be archived
and nothing is lost. yoke holds the list, threads write to it, and the board
shows what waits for his go.

## Words

- **Ticket**: an EPD Tasks page in Notion assigned to Ashvin, in the current
  sprint (the Sprints page whose Sprint status is Current). The team's unit
  of work. It lives in Notion; yoke keeps a linked copy.
- **Work**: one thing Ashvin asked a thread to do. It ends in PRs, or in an
  answer when there's nothing to ship.
- **Finding**: something a thread noticed and didn't do: a bug, a follow-up, a
  cleanup, a question for later. It waits for his go.
- **Go**: Ashvin saying a finding should be done. Until then it is only
  recorded, unless it is small.
- **Small**: a finding in code an open PR of this thread already touches, a
  few lines, no behaviour anyone relies on changes, and no design choice: a
  typo, dead code, a wrong comment, a missing assertion. The thread fixes it
  in that PR without waiting for his go.
- **Track**: one of his workstreams in `tracks.yaml`. Every task carries one.
- **Thread**: a desktop session. It does work, records findings, and is
  archived once its work is done.

## What yoke holds

| Kind | Parent | Status | Tags | Holds | Notion |
|---|---|---|---|---|---|
| Ticket | none | from Notion: `pending`, `in_progress`, `blocked`, `done` | `ticket`, track | the Notion link | linked; status pulled, never pushed |
| Work | its ticket, or none | `in_progress` → `done` or `dropped` | `work`, track, repo | his ask in his words, the thread, PRs as notes, an outcome line | local only |
| Finding | the ticket it came from, or none | `pending` (waiting for go) → `active` (go) → `in_progress` → `done` or `dropped`; a small one goes straight to `done` | `finding`, track | what, where, why it matters, how to pick it up, the thread that found it | local only |

yoke pushes only tasks linked to a Notion page, so work and findings never
reach the team's board.

## Who does what

| When | What happens | Done by |
|---|---|---|
| Every hour | Tickets assigned to Ashvin in the current sprint come into yoke; every linked ticket still open picks up its Notion status, even after its sprint ends | ox's hook runs a new `yoke sync` |
| A thread starts on a request | Work under the ticket its CB id names (from his words, the branch or the PR title); top-level when there's no ticket | the thread |
| A thread notices something small | Fixed in the PR that touches it; finding recorded as `done` with that PR | the thread |
| A thread notices something it won't do now | Finding, `pending` | the thread |
| A thread opens a PR | PR noted on its work | open-pr |
| A PR merges | Work `done` once all its PRs merged; outcome from the PR titles | ox, hourly |
| Work with no PR ends | Work `done`, with a one-line outcome | the thread, on its last turn |
| Ashvin says go | Finding `active`; started in that thread or picked up by another | the thread he says it in |
| Ashvin says drop | Finding `dropped` | the thread he says it in |
| A ticket is done in Notion | yoke's ticket `done`; open findings under it stay for him to decide | the sync |

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
Once a thread knows its ticket, it reads `yoke context <ticket>`: earlier work
and outcomes, findings, notes.

## Not in scope

- Writing anything back to Notion. Ticket status stays the team's.
- Picking work up from Slack's To-dos list. The Slack connector can't read
  it, and scraping Slack's web page would break easily (2026-11-05).
- A model reading transcripts to judge whether a thread is done.

## Decided (2026-11-05)

1. Tickets: the current sprint only.
2. Small findings: a thread fixes them without waiting for his go.
3. Archiving: the routine archives on its own, then lists what it archived.
4. Board: one line per ticket with findings waiting.
5. Findings without a ticket: top-level tasks with the track (default).
6. Notion: never written to; starting work doesn't move a ticket (default).
