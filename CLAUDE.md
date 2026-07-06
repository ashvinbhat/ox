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

## Testing Commands

**Workspace-dependent commands must be run FROM the workspace directory:**
- `ox checkpoint` - requires being in workspace
- `ox checkpoints` - requires being in workspace
- `ox resume` - requires being in workspace
- `ox refresh` - requires being in workspace
- `ox morph` - requires being in workspace
- `ox skill inject/eject` - requires being in workspace

These commands use `getCurrentWorkspace()` which detects if the current working directory is inside a workspace path.

**Commands that work from anywhere:**
- `ox pickup` - creates workspace
- `ox status` - lists all workspaces
- `ox done <task-id>` - can specify task ID
- `ox repo list` - lists registered repos
- `ox skill list` - lists available skills
- `ox personas` - lists personas
- `ox hooks` - lists hooks
- `ox learn` - works anywhere (but adds workspace context if in one)
- `ox learnings` - lists all learnings

## Yoke Integration

ox is fully decoupled from yoke's code — no library import. All task data flows
through the yoke CLI:

- `internal/yokecli` is the ONLY bridge: it shells out to the yoke binary and
  decodes `--json` output (show/list/notes + start/done/note/context/docs)
- Task management is done with yoke directly (`yoke add/list/show/...`), not
  through ox wrappers — those were removed
- Every workspace gets a `YOKE.md` symlink → `~/.yoke/AGENTS.md` (regenerated
  by `yoke docs`) so agents know how to drive yoke
- yoke binary location: `~/go/bin/yoke` (yokecli.BinaryPath falls back there
  when PATH lacks it)
- yoke database: `~/.yoke/yoke.db`
- Creating tasks: `yoke add "title" -t tag1 -t tag2 -p 2` (NOT `--tags`)

## Common Issues

1. **"unknown command" errors** - Binary is stale, rebuild with `go build -o ox ./cmd/ox`

2. **"not in a workspace" errors** - Run command from workspace directory:
   ```bash
   cd ~/.ox/tasks/<task-dir>
   ox checkpoint --done "..."
   ```

3. **Config not found** - Run `ox init` first

4. **Repo not registered** - Run `ox repo add <url> --name <name>`

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
├── tasks/               # Legacy solo workspaces (ox pickup)
├── skills/              # Skill markdown files
├── personas/            # Persona definitions
├── hooks/               # Claude Code hooks
└── learnings.db         # Legacy learnings (migrated into memory.db)
```

## Full Workflow Test

```bash
# 1. Create task in yoke
yoke add "Test task" -t backend -p 2

# 2. Pickup task
ox pickup <task-id> --repos backend

# 3. Work in workspace
cd ~/.ox/tasks/<task-dir>
ox checkpoint --done "Did X" --next "Do Y"
ox learn "Learned Z" -c gotcha

# 4. Complete
ox done <task-id> --reason "Shipped"
```
