# ox

Ashvin opens the Claude Code desktop app in this folder, with `~/.ox/` added,
and starts threads for work in any repo. Most threads here are not about ox.
Every thread: read "Working from this folder". Threads that change ox itself:
also read "Building ox".

## Working from this folder

- Starting work is just opening a thread and saying what to do. No `ox go`,
  no task to create first.
- Each thread starts with `ox now --brief` (the `ox-brief` SessionStart hook):
  what needs Ashvin, what's in review, what's running. It is background: don't
  bring it up unless he asks what needs him or the thread's work touches it.
  `ox now` is the full view, `--json` for parsing, `--track <short>` to filter.
- Tracks are his workstreams, in `~/.ox/tracks.yaml`.
- Repos are cloned at `~/.ox/repos/<name>` (backend, frontend, python-backend,
  api-automation, infra-configurations, …). Desktop worktree threads live at
  `<repo>/.claude/worktrees/<name>`.
- Tasks: drive yoke directly — `yoke ready`, `yoke show <id>`,
  `yoke add "title" -t tag1 -t tag2 -p 2` (not `--tags`). Take the task number
  from `yoke add` output.
- Memory: Claude auto-memory for this folder is the source of truth.
  `ox memory recall <query>` searches the older `~/.ox/memory.db`.
- PR reviews happen only when Ashvin asks, against the whole codebase at the
  PR head, not just the diff. Past review rounds per PR are in `~/.ox/reviews/`.
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
- `ox hooks init` — install the `ox-brief` hook (retires pre-1.0 hooks)
- `ox worktrees prune [--dry-run]` — remove worktrees whose PR merged (the
  hook runs it hourly in the background; log in `~/.ox/logs/worktrees.log`)
- `ox repo add/list`, `ox personas`, `ox learn` / `ox learnings`, `ox memory …`
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
- `internal/ghreview` — per-PR review rounds (head SHA, findings) in
  `~/.ox/reviews/<owner>-<repo>-<pr>.json`.
- `internal/memory` — `~/.ox/memory.db` (FTS5 + embeddings, hybrid RRF recall).
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
├── memory.db        # Older long-term memory store
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
