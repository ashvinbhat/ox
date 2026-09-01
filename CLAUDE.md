# Ox - Agent Instructions

## The Harness (v2 core)

ox's center is the mission harness: `ox go <task-ref|mission-id|"goal">` opens a
persistent orchestrator (interactive claude, tmux window "orc") that plans
conversationally, spawns workers/jobs only when the delegation ladder justifies
it, integrates, and ships — resumable at any time (`claude --resume` via
pre-assigned session ids; the task is the resume handle).

- Missions live at `~/.ox/missions/m<seq>-<slug>/` — mission.yaml, plan.md,
  decisions.md, scratchpad.md, events.jsonl, ledger.jsonl, agents.json,
  jobs.json, workers/, jobs/. Files are truth; every process is stateless.
- `ox mcp --mission <id> --role orchestrator|worker` serves the typed tool
  surface per claude session (wired via --mcp-config --strict-mcp-config)
- The watcher (window "watch", `ox watch <id>`) reconciles state, auto-starts
  dependency-gated workers, prices transcripts into the ledger, enforces
  budgets, and injects digests into the orc pane (single-injector, guarded)
- Memory: ~/.ox/memory.db (FTS5 + embeddings, hybrid RRF recall), repo docs at
  ~/.ox/memory/repos/, distiller runs at mission close
- Playbooks (mission types) are markdown at ~/.ox/playbooks/ overriding
  embedded defaults (task, debug) — new type = new file, zero Go

## Building

After any code changes, rebuild the binary:
```bash
go build -o ox ./cmd/ox
```

Or use `go run ./cmd/ox <command>` during development.

## Commands

The mission harness is the primary surface:
- `ox` — resume the current mission (bare `ox` = `ox go` with no args)
- `ox go <task-ref | mission-id | "goal">` — start or resume a mission
- `ox watch <id>` — reconcile + budget + digest loop for a mission
- `ox missions` / `ox where <id>` — list missions / locate its worktrees & agents
- `ox inbox` — what needs the human · `ox retro` — cost/health · `ox plan` — review plan.md
- `ox mcp --mission <id> --role orchestrator|worker` — typed tool server per session
- `ox opencode [dir]` — open opencode with ox provider keys wired in

Work-anywhere helpers:
- `ox repo add/list` — manage registered codebases
- `ox personas` — list personas (agent mindset + model preset)
- `ox hooks` — list Claude Code hooks
- `ox learn` / `ox learnings` — write / search long-term memory
- `ox memory …` — inspect the memory store

The pre-mission "solo workspace" commands (pickup/checkpoint/morph/skill/…) and
their `internal/{agent,tui,workspace,skills}` packages were removed — missions
replaced them, and skills/hooks now use Claude Code's native mechanisms.

## Yoke Integration

ox is fully decoupled from yoke's code — no library import. All task data flows
through the yoke CLI:

- `internal/yokecli` is the ONLY bridge: it shells out to the yoke binary and
  decodes `--json` output (show/list/notes + start/done/note/context/docs)
- Task management is done with yoke directly (`yoke add/list/show/...`), not
  through ox wrappers — those were removed
- Every mission dir gets a `YOKE.md` symlink → `~/.yoke/AGENTS.md` (regenerated
  by `yoke docs`) so agents know how to drive yoke
- yoke binary location: `~/go/bin/yoke` (yokecli.BinaryPath falls back there
  when PATH lacks it)
- yoke database: `~/.yoke/yoke.db`
- Creating tasks: `yoke add "title" -t tag1 -t tag2 -p 2` (NOT `--tags`)

## Common Issues

1. **"unknown command" errors** - Binary is stale, rebuild with `go build -o ox ./cmd/ox`

2. **Config not found** - Run `ox init` first

3. **Repo not registered** - Run `ox repo add <url> --name <name>`

## Architecture

```
~/.ox/
├── ox.yaml              # Config (repos, models tiers, memory.embeddings)
├── repos/               # Cloned repos (base)
├── worktrees/           # Git worktrees: <repo>/m<seq>-<agent>, m<seq>-integration
├── missions/            # Harness missions (the primary work unit)
│   └── m<seq>-<slug>/   # mission.yaml, plan.md, events.jsonl, workers/, jobs/...
├── memory.db            # Long-term memory (FTS5 + embedding BLOBs)
├── memory/repos/        # Living per-repo knowledge docs (distiller-maintained)
├── playbooks/           # Mission-type overrides (task.md, debug.md, ...)
├── personas/            # Persona definitions
└── hooks/               # Claude Code hooks
```

## Full Workflow Test

```bash
# 1. Create task in yoke
yoke add "Test task" -t backend -p 2

# 2. Open a mission on it (orchestrator plans, delegates, integrates, ships)
ox go <task-id>

# 3. Leave and resume any time — the task/mission id is the resume handle
ox go <task-id>     # or just: ox
```
