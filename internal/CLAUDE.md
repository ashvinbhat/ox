# Building ox: reference

What a thread changing ox needs on top of the root CLAUDE.md's "Building ox".
It sits here so threads that aren't about ox don't load it.

## Commands

- `ox` / `ox now` — threads by state and track, yoke tasks, PRs without a thread
- `ox setup` — install the `ox-brief` hook and check it runs (rolling back
  `settings.json` if not), ox's skills and the board panel; re-run after
  `make install`
- `ox reviews`, `ox review ask|start|post|record` — the review desk the
  `/review-pr` and `/review-asks` skills drive
- `ox worktrees prune [--dry-run]` — remove worktrees whose PR merged (the
  hook runs it hourly in the background; log in `~/.ox/logs/worktrees.log`)
- `ox memory audit [--json] [--keep <id>]` — gone paths, code and ox commands,
  cited files changed since the memory was confirmed, near-duplicate
  memories, broken frontmatter, MEMORY.md drift, stale project memories,
  memories stranded in other projects' stores, always-loaded text over 3,000
  words (the hook runs it weekly; the
  result is in `~/.ox/memory-check/last.json`)
- `ox asks [--json] [--offline]`, `ox asks drop <id>`, `ox asks refresh` —
  the queue of what waits on Ashvin; PR state is cached in `~/.ox/asks/` and
  refreshed by the hook and the board once it's 10 minutes old
- `ox journal [--days N] [--track x]`, `ox journal add|own-it|recap|status` —
  the decision journal the CLAUDE.md rules, `/own-it` and `/recap` write and read
- `ox work refresh [--if-due] [--dry-run]` — `yoke sync` (hourly), then a
  yoke task for every thread with a PR: found by `thread:<id>` and
  `pr:<repo>-<n>` keys, under the ticket a PR names, started while a PR is
  open, done once its PRs merged (every 10 minutes; log in
  `~/.ox/logs/work.log`)
- `ox work archivable [--json]`, `ox work archived <id>...` — the threads
  the daily routine archives (finished, not parked, no open PR, work done,
  own worktree clean and pushed, quiet 24h) and its record of what it
  archived, which the brief shows. The `ox daily archive` scheduled task runs
  the `daily-archive` skill
- `ox park --next "…"` — mark this thread parked; `/park` runs it after the
  resumption note. `ox now` shows it as parked with that step until the thread
  is active again; it's never an ask or archivable
- `ox repo add/list`, `ox personas`, `ox learnings` / `ox memory recall` (the
  memory.db archive)
- `ox opencode [dir]` — opencode with ox provider keys wired in
- Frozen mission harness: `ox go`, `ox watch`, `ox missions`, `ox where`,
  `ox inbox`, `ox retro`, `ox plan`, `ox mcp`

## Code map

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
- `internal/work` — plans the yoke commands that keep each thread's work:
  keys, notes per PR, start, done. `internal/cli/work.go` decides which
  thread a PR belongs to (the one that opened it) and runs the plan.
- `internal/asks` — the asks queue: sources in, ranked asks and thread fixes
  out (`Build`), PR state from gh (`FetchPRs`, cached), drops that lapse when
  the ask changes.
- `internal/journal` — the decision journal: day files in `~/.ox/journal/`
  (`### HH:MM · track · thread` then `- Key: value` lines; the first key
  names the kind), parsing, and the status the brief shows (recap due,
  own-it rounds today).
- `internal/memory` — `~/.ox/memory.db`, the archive (FTS5 + embeddings).
- `internal/hooks` — registers the start hook (`ox hook session-start`, in
  `internal/cli/hook.go`) and any custom scripts in `~/.ox/hooks/` in
  `~/.claude/settings.json`, and runs setup's check with rollback.
- `internal/jobs` — background job status and locks in `~/.ox/jobs/`: two
  clocks (last success sets the cadence, last attempt paces retries), a pid
  lock with stale takeover, a time limit; `Failing` feeds "ox problem:".
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
├── jobs/            # Background job status and locks
├── parked/          # Threads left on purpose, with the next step
├── archived/        # What the daily routine archived, one file a day
├── memory-check/    # Last memory audit and kept suggestions
├── memory.db        # Memory archive from before 1.0
├── memory/repos/    # Per-repo knowledge docs
├── hooks/           # Claude Code hook scripts
├── missions/        # Frozen mission harness state
└── worktrees/       # Mission worktrees
```

## Common issues

1. **"unknown command"** — the binary is stale: `make install`.
2. **Config not found** — run `ox init`.
3. **`ox now` shows no threads** — the desktop app's state dir moved or changed
   format; check `internal/desktop.DefaultDir`.
