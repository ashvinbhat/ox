# ox

Ashvin opens the Claude Code desktop app in this folder, with `~/.ox/` added,
and starts threads for work in any repo. Most threads here are not about ox.
Every thread: read "Working from this folder". Threads that change ox itself:
also read "Building ox".

## Working from this folder

- Ashvin chats in plain words. He doesn't run `ox` or slash commands and
  won't remember them: they're yours. Map what he says to the command or
  skill, run it, and answer in prose. Never end with "run `ox …`" or "type
  /…"; do it.
  - "what's waiting on me?" → `ox asks`; "where are things?" → `ox now`
  - "what did we decide today / this week?" → `ox journal [--days 7]`
  - "let me own this" → own-it; "recap" → recap; "clean up my threads" → tidy
  - "clean up the board" → tidy, board items only: drop questions that repeat
    another thread's, archive threads that finished, pass on his answers
  - "check my memory" → memory-check; "any review asks?" → review-asks;
    "review <PR>" → review-pr; "park this" / "I'll come back to this" → park
  - "open the PR" / "put it up" → open-pr: draft first, ready once the
    targeted tests pass locally
- Starting work is just opening a thread and saying what to do. No `ox go`,
  no task to create first.
- Stops: ask Ashvin only for a stop in `~/.ox/stops.md` (Send, Delete,
  Merge, Call, Fact), and start the question with its kind: "Send: post this
  reply in #backend?". Everything else: do it, then say what you did and how
  to undo it. The `ox hook ask` hook sends any other question back. A go he
  gave covers the steps it names.
- Each thread starts with `ox now --brief` (the `ox-brief` SessionStart hook,
  `ox hook session-start`):
  the top of what's waiting on Ashvin, PR fixes threads can make, what's in
  review, what's running. It is background: don't bring it up unless he asks
  what needs him or the thread's work touches it. One exception: a "Mention
  once" line (only the first thread of the day gets it). Say it in one
  sentence at the end of your first reply ("Also: the weekly recap is
  due."). An "ox problem:" line is a background job
  that keeps failing; raise it only when it blocks this thread's work. `ox now`
  is the full view, `--json` for parsing, `--track <short>` to filter.
- The ox board is a side panel in every desktop thread (the `ox-board` plugin
  `ox setup` installs): Needs you (threads stopped on him, with the stop's
  kind), In progress (threads with work moving, by ticket) and Landed today.
  A click on an item opens its thread; ⏲️ snoozes a stop for a day, ×
  dismisses it until its thread asks something new.
- Tracks are his workstreams, in `~/.ox/tracks.yaml`.
- Repos are cloned at `~/.ox/repos/<name>` (backend, frontend, python-backend,
  api-automation, infra-configurations, …). Desktop worktree threads live at
  `<repo>/.claude/worktrees/<name>`.
- Tasks: drive yoke directly — `yoke ready`, `yoke show <id>`,
  `yoke add "title" -t tag1 -t tag2 -p 2` (not `--tags`). Take the task number
  from `yoke add` output. Tickets come in from Notion on their own; read
  `yoke context CB-<n>` once you know a thread's ticket.
- Lasting work gets a task. ox keeps the task of a thread with a PR. Lasting
  work without one (an investigation's answer, a design, a send, a delete)
  you add: `yoke add "<his ask>" -k <this thread's key> -t work -t <track>`,
  the key from the brief, and `yoke done <id> --outcome "…"` on its last
  turn. A lookup that changes nothing isn't lasting; unsure means lasting.
- Anything you notice and don't do, and any later step you promise ("I'll
  check X after the merge"), is a finding, added in the same reply:
  `yoke add "…" -t finding -t <track> --from <work> -b "what, where, why,
  how to pick it up"`. A small one in code your open PR touches, fix there.
  Findings are open work any thread may pick up; his drop makes one
  `dropped`.
- Memory: Claude auto-memory for this folder is the one store. When a memory
  rests on specific code or files, add `metadata.cites`
  (`["backend:SegmentService", "ox:internal/cli/now.go", "~/.ox/tracks.yaml"]`)
  so the weekly audit checks exactly those. When Ashvin confirms a memory
  still holds, set `metadata.verified: YYYY-MM-DD`. Write a feedback memory
  when he states a rule or corrects the same thing twice; log a one-off
  correction with `ox journal add`. When he corrects something a feedback
  memory covers, add the day to its `metadata.misses`; the audit then asks
  to move the rule up to its skill, then a check. `/memory-check` works
  through what the audit found. `~/.ox/memory.db` is an archive
  (`ox memory recall <query>`).
- Asks: `ox asks` is one ranked queue of what only Ashvin can do (thread
  questions, review asks, PRs ready to merge, memory, recap), plus PR fixes
  threads can make. On his PRs, resolve conflicts, fix CI your change broke
  and review comments yourself; make design calls, log them and name them,
  unless one is a Call; never break main. `/tidy` archives finished threads,
  renames duplicates, files threads into a sidebar group per track and
  clears board items, then reports what it did.
- Decisions: when you make a real choice (an approach, a design, a trade-off,
  the root cause you act on), log it right then:
  `ox journal add --decided "…" --over "…" --why "…"`, a line each in his
  style. ox adds the time, the thread and its track. Name those decisions in
  your closing message too. Skip routine choices (names, commands). The
  journal is `~/.ox/journal/YYYY-MM-DD.md`; `ox journal` reads today.
- Own-it: tracks with `own: true` in `tracks.yaml` (durable, ox) are the ones
  he wants to understand deeply. When he asks, run a round (2 a day, `ox
  journal status`): he predicts the approach, you build, then compare
  (`/own-it`). Don't offer one.
- Recap: `/recap` takes three concepts from the week's journal and asks him
  to explain each back. The brief shows "Recap due" Friday through Monday.
- PR reviews happen only when Ashvin asks, against the whole codebase at the
  PR head, not just the diff: `/review-pr <url>`. `/review-asks` picks up asks
  from Slack; `ox reviews` is the queue. Rounds and asks per PR are in
  `~/.ox/reviews/`.
- Missions (`ox go`, the tmux orchestrator, `~/.ox/missions/`) are frozen:
  they still run, but don't start new work in them.

## Building ox

ox is heading to 1.0.0: what desktop threads draw on. Every change ships as a
PR to ashvinbhat/ox (main is protected; Ashvin merges) with an entry under
`[Unreleased]` in CHANGELOG.md.

```bash
go build -o ox ./cmd/ox    # dev build
make install               # version-stamped build into ~/go/bin/ox
go test ./...
make mutate                # each rule in tools/mutate/mutations.yaml, off, fails a test
```

A test that guards a rule gets a `mutations.yaml` entry. Tests get paths
from `t.TempDir()`: `internal/livepath` panics when one reaches the real
home.

Hooks and MCP servers keep the binary they started with: restart threads after
`make install`.

The commands, code map, `~/.ox` layout and common issues are in
`internal/CLAUDE.md`. It loads when you open a file under `internal/`; read it
before planning an ox change.
