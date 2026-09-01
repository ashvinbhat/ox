# ox

> A mission harness for AI-assisted development — built on [yoke](https://github.com/ashvinbhat/yoke)

`ox go <task>` opens a persistent orchestrator that plans with you, delegates to
workers and jobs only when the work justifies it, integrates, and ships — all in
one tmux session you can leave and resume at any time.

## Features

- **Mission-centric**: one resumable orchestrator per task; the task id is the resume handle
- **Files are truth**: every mission's state lives on disk, so every process is stateless and restart-safe
- **Delegation ladder**: spawns worker sessions and headless jobs (claude or opencode) only when needed
- **Self-reconciling**: a watcher auto-starts dependency-gated workers, prices transcripts into a ledger, and enforces budgets
- **Long-term memory**: hybrid FTS5 + embedding recall, distilled at mission close
- **Playbooks**: mission types (task, debug, review, …) are plain markdown, no code

## How It Works

```
tmux session  ox-<mission>
├─ window "orc"    persistent claude orchestrator — plans, delegates, integrates, ships
└─ window "watch"  reconciles state, starts gated workers, prices the ledger, injects digests

~/.ox/missions/m<seq>-<slug>/     mission.yaml · plan.md · events.jsonl · ledger.jsonl
                                  agents.json · jobs.json · workers/ · jobs/
```

Workers are their own claude/opencode sessions in git worktrees; jobs are
detached headless one-shots. Both are addressed by the files in the mission
dir, so the orchestrator, watcher, and MCP server are all stateless over them.

## Installation

```bash
go install github.com/ashvinbhat/ox/cmd/ox@latest
ox init
```

Requires [yoke](https://github.com/ashvinbhat/yoke) installed and initialized,
plus [Claude Code](https://claude.com/claude-code) and tmux.

## Quick Start

```bash
ox init                                           # Initialize ~/.ox
ox repo add git@github.com:company/backend.git --name backend
ox go 114                                         # Open/resume the mission for task #114
ox                                                # Resume the current mission
```

## Commands

### Missions (the primary workflow)
```bash
ox                               # Resume the current mission (bare ox = ox go, no args)
ox go 114                        # Open/resume the mission for a task
ox go m17                        # Resume a mission by id
ox go "why is CI flaky" --playbook debug   # Freeform mission
ox go <pr-url>                   # Review a PR (review playbook)

ox missions                      # List missions with phase + spend
ox missions distill m17          # Re-run knowledge distillation
ox missions prune                # GC zombie sessions + closed-mission worktrees
ox where m17                     # Show where a mission's code and files live
ox inbox                         # What needs you, across all missions
ox retro                         # Mission scorecards — cost and outcome
ox plan                          # Review a mission's plan from the terminal
ox watch m17                     # Run the watcher loop (normally its own window)
```

### Models / opencode
```bash
ox opencode [dir]                # Open opencode with ox provider keys wired in
```
Workers and jobs default to claude; the orchestrator can put one on opencode
(any OpenRouter/Google model) when you ask.

### Memory
```bash
ox memory recall "query"         # Search long-term memory
ox memory stats|migrate|backfill|gc
ox learn "insight" [-t tag]      # Capture a learning
ox learnings [--tag x]           # List captured learnings
```

### Repos, personas, hooks, budgets
```bash
ox repo add <url> [--name x]     # Register a codebase
ox repo list | remove <name>
ox personas                      # List personas (agent mindset + model preset)
ox hooks | hooks init            # Manage Claude Code hooks
ox budgets on|off                # Toggle spend enforcement (tracking always on)
```

### Task Management (yoke)

Task management lives entirely in yoke — use it directly:

```bash
yoke add "title"               # Create a new task
yoke list / yoke ready         # List tasks / unblocked tasks
yoke show <id>                 # Task details
yoke context <id>              # Full task context (markdown)
yoke note <id> "text"          # Add note
yoke tree                      # Show task hierarchy
yoke done <id>                 # Complete task
```

`yoke docs` writes the full usage reference to `~/.yoke/AGENTS.md`; ox symlinks
it into each mission dir as `YOKE.md` so agents always know how to drive yoke.

## Personas

| Persona | Role | Auto-triggers |
|---------|------|---------------|
| **captain** | Orchestrates, plans, delegates | `epic`, `project`, `plan`, `design` |
| **builder** | Implements, ships code | `bug`, `feature`, `fix`, `implement` |
| **explorer** | Researches, investigates | `research`, `spike`, `investigate` |
| **reviewer** | Reviews, checks quality | `review`, `pr`, `audit`, `quality` |

A persona is a system-prompt + model preset applied to a worker or job.

## Hooks

ox installs SessionStart hooks into Claude Code's native hook system
(`~/.claude/settings.json`) to surface ready yoke tasks and an ox quick
reference. Run `ox hooks init` to install them.

## Directory Structure

```
~/.ox/
├── ox.yaml              # Config (repos, model tiers, memory.embeddings, budgets)
├── repos/               # Registered codebases (base clones)
├── worktrees/           # Git worktrees: <repo>/m<seq>-<agent>, m<seq>-integration
├── missions/            # Missions — the primary work unit
│   └── m<seq>-<slug>/   # mission.yaml, plan.md, events.jsonl, workers/, jobs/…
├── memory.db            # Long-term memory (FTS5 + embedding BLOBs)
├── memory/repos/        # Living per-repo knowledge docs (distiller-maintained)
├── playbooks/           # Mission-type overrides (task.md, debug.md, …)
├── personas/            # Persona definitions
└── hooks/               # Claude Code hooks
```

## Configuration

```yaml
# ~/.ox/ox.yaml
repos:
  backend:
    url: git@github.com:company/backend.git
    base_branch: main
    copy_files: [.env, .vscode]
    post_setup: npm install

models:
  orchestrator: claude-opus-4-6
```

## Dependencies

- [yoke](https://github.com/ashvinbhat/yoke) — task management
- Git with worktree support
- [Claude Code](https://claude.com/claude-code) CLI and tmux (for the mission harness)
- [opencode](https://opencode.ai) — optional, for running workers/jobs on other models

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). `main` is protected — changes land via
pull requests, and CI (build + vet + test) must pass.

## License

[MIT](./LICENSE)
