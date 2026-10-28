# ox

Ashvin opens the Claude Code desktop app in this folder, with `~/.ox/` added,
and starts threads for work in any repo. Most threads here are not about ox.
Every thread: read "Working from this folder". Threads that change ox itself:
also read "Building ox".

## Working from this folder

- Ashvin chats in plain words. He doesn't run `ox` or slash commands and
  won't remember them: they're yours. Map what he says to the command or
  skill, run it, and answer in prose. Never end with "run `ox …`" or "type
  /…"; offer in words and do it on a yes.
  - "what's waiting on me?" → `ox asks`; "where are things?" → `ox now`
  - "what did we decide today / this week?" → `ox journal [--days 7]`
  - "let me own this" → own-it; "recap" → recap; "clean up my threads" → tidy
  - "check my memory" → memory-check; "any review asks?" → review-asks;
    "review <PR>" → review-pr; "park this" / "I'll come back to this" → park
- Starting work is just opening a thread and saying what to do. No `ox go`,
  no task to create first.
- Each thread starts with `ox now --brief` (the `ox-brief` SessionStart hook):
  the top of what's waiting on Ashvin, PR fixes threads can make, what's in
  review, what's running. It is background: don't bring it up unless he asks
  what needs him or the thread's work touches it. One exception: a "Mention
  once" line (only the first thread of the day gets it). Say it as a
  one-sentence offer at the end of your first reply ("Also: the weekly recap
  is due. Want to do it now?"). `ox now` is the full view, `--json` for
  parsing, `--track <short>` to filter.
- The ox board is a side panel in every desktop thread (the `ox-board` plugin
  `ox setup` installs): the threads that need him, each with one line on
  what it's waiting on. It's how he sees what's waiting without asking.
- Tracks are his workstreams, in `~/.ox/tracks.yaml`.
- Repos are cloned at `~/.ox/repos/<name>` (backend, frontend, python-backend,
  api-automation, infra-configurations, …). Desktop worktree threads live at
  `<repo>/.claude/worktrees/<name>`.
- Tasks: drive yoke directly — `yoke ready`, `yoke show <id>`,
  `yoke add "title" -t tag1 -t tag2 -p 2` (not `--tags`). Take the task number
  from `yoke add` output.
- Memory: Claude auto-memory for this folder is the one store. When a memory
  rests on specific code or files, add `metadata.cites`
  (`["backend:SegmentService", "ox:internal/cli/now.go", "~/.ox/tracks.yaml"]`)
  so the weekly audit checks exactly those. When Ashvin confirms a memory
  still holds, set `metadata.verified: YYYY-MM-DD`. `/memory-check` works
  through what the audit found. `~/.ox/memory.db` is an archive
  (`ox memory recall <query>`).
- Asks: `ox asks` is one ranked queue of what only Ashvin can do (thread
  questions, review asks, PRs ready to merge, memory, recap), plus PR fixes
  threads can make. On his PRs, resolve conflicts, fix CI your change broke
  and small review comments yourself; ask before any design, architecture,
  big or breaking change, and never break main. `/tidy` archives finished
  threads, renames duplicates and files threads into a sidebar group per
  track, after his OK.
- Decisions: when you make a real choice (an approach, a design, a trade-off,
  the root cause you act on), log it right then:
  `ox journal add --decided "…" --over "…" --why "…"`, a line each in his
  style. ox adds the time, the thread and its track. Name those decisions in
  your closing message too. Skip routine choices (names, commands). The
  journal is `~/.ox/journal/YYYY-MM-DD.md`; `ox journal` reads today.
- Own-it: tracks with `own: true` in `tracks.yaml` (durable, ox) are the ones
  he wants to understand deeply. On those, before writing code, offer one
  own-it round if `ox journal status` shows rounds left today (2 a day):
  he predicts the approach, you build, then compare (`/own-it`). Run it
  anywhere when he asks.
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
```

Hooks and MCP servers keep the binary they started with: restart threads after
`make install`.

### Commands

- `ox` / `ox now` — threads by state and track, yoke tasks, PRs without a thread
- `ox setup` — install the `ox-brief` hook (retiring pre-1.0 hooks), ox's
  skills and the board panel; re-run after `make install`
- `ox reviews`, `ox review ask|start|post|record` — the review desk the
  `/review-pr` and `/review-asks` skills drive
- `ox worktrees prune [--dry-run]` — remove worktrees whose PR merged (the
  hook runs it hourly in the background; log in `~/.ox/logs/worktrees.log`)
- `ox memory audit [--json] [--keep <id>]` — gone paths, code and ox commands,
  cited files changed since the memory was confirmed, near-duplicate
  memories, broken frontmatter, MEMORY.md drift, stale project memories,
  memories stranded in other projects' stores (the hook runs it weekly; the
  result is in `~/.ox/memory-check/last.json`)
- `ox asks [--json] [--offline]`, `ox asks drop <id>`, `ox asks refresh` —
  the queue of what waits on Ashvin; PR state is cached in `~/.ox/asks/` and
  refreshed by the hook and the board once it's 10 minutes old
- `ox journal [--days N] [--track x]`, `ox journal add|own-it|recap|status` —
  the decision journal the CLAUDE.md rules, `/own-it` and `/recap` write and read
- `ox park --next "…"` — mark this thread parked; `/park` runs it after the
  resumption note. `ox now` shows it as parked with that step until the thread
  is active again; it's never an ask or archivable
- `ox repo add/list`, `ox personas`, `ox learnings` / `ox memory recall` (the
  memory.db archive)
- `ox opencode [dir]` — opencode with ox provider keys wired in
- Frozen mission harness: `ox go`, `ox watch`, `ox missions`, `ox where`,
  `ox inbox`, `ox retro`, `ox plan`, `ox mcp`

### Code map

- `internal/desktop` — reads the desktop app's per-thread state files under
  `~/Library/Application Support/Claude/claude-code-sessions/`. The format is
  internal to the app: every field is optional, unparseable files are skipped.
- `internal/tracks` — `tracks.yaml` loading; a term matches at a word start.
- `internal/yokecli` — the ONLY bridge to yoke: shells out to the yoke binary
  (`~/go/bin/yoke` fallback) and decodes `--json`. No yoke library import.
- `internal/ghreview` — the review ledger `~/.ox/reviews/<owner>-<repo>-<pr>.json`
  (asks, rounds with head/base SHA, findings, verdicts), prepare/submit/queue
  in `desk.go`, diff files in `~/.ox/reviews/work/` (outside the checkout).
- `internal/claudeskills` — ox's skills (`*/SKILL.md`, embedded) and the
  `ox-board` panel plugin (`hooks/register.tsx`; `claude plugin test` runs its
  tests), installed by `ox setup` into `~/.claude/skills`. `owner: ox` in a
  SKILL.md and `"author": { "name": "ox" }` in a plugin.json mark them as ox's.
- `internal/memcheck` — the memory audit: frontmatter parsing, the checks,
  and the code search (ripgrep over checkouts, then `git grep` at the default
  branch for what's still missing). Keeps and the last report are in
  `~/.ox/memory-check/`.
- `internal/park` — parked threads, one record each in `~/.ox/parked/`.
- `internal/asks` — the asks queue: sources in, ranked asks and thread fixes
  out (`Build`), PR state from gh (`FetchPRs`, cached), drops that lapse when
  the ask changes.
- `internal/journal` — the decision journal: day files in `~/.ox/journal/`
  (`### HH:MM · track · thread` then `- Key: value` lines; the first key
  names the kind), parsing, and the status the brief shows (recap due,
  own-it rounds today).
- `internal/memory` — `~/.ox/memory.db`, the archive (FTS5 + embeddings).
- `internal/hooks` — SessionStart hook scripts in `~/.ox/hooks/`, registered
  in `~/.claude/settings.json`.
- Frozen harness: `internal/{harness,mission,watcher,job,mcpserver}`. A mission
  is `~/.ox/missions/m<seq>-<slug>/` (mission.yaml, plan.md, events.jsonl,
  ledger.jsonl, agents.json, jobs.json); files are truth, every process is
  stateless; `ox mcp` serves the typed tool surface per claude session.

```
~/.ox/
├── ox.yaml          # Config (repos, model tiers, memory.embeddings)
├── tracks.yaml      # Workstreams for ox now
├── repos/           # Cloned repos
├── reviews/         # Review rounds per PR
├── journal/         # Decision journal, one file a day
├── asks/            # PR state cache and dropped asks
├── parked/          # Threads left on purpose, with the next step
├── memory-check/    # Last memory audit and kept suggestions
├── memory.db        # Memory archive from before 1.0
├── memory/repos/    # Per-repo knowledge docs
├── hooks/           # Claude Code hook scripts
├── missions/        # Frozen mission harness state
└── worktrees/       # Mission worktrees
```

### Common issues

1. **"unknown command"** — the binary is stale: `make install`.
2. **Config not found** — run `ox init`.
3. **`ox now` shows no threads** — the desktop app's state dir moved or changed
   format; check `internal/desktop.DefaultDir`.
