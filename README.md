# ox

> What your Claude Code threads draw on — built on [yoke](https://github.com/ashvinbhat/yoke)

You work in Claude Code desktop threads. ox keeps them pointed the same way:
every thread starts knowing what needs you, what's in review and what's
running, grouped by your tracks.

## Features

- **`ox now`**: every desktop thread by state — needs you, in review, working,
  stuck, done — tagged with its track, plus yoke tasks and open PRs no thread owns
- **Start brief**: a SessionStart hook gives each new thread `ox now --brief`
- **Tracks**: your workstreams in `~/.ox/tracks.yaml`
- **Long-term memory**: hybrid FTS5 + embedding recall in `~/.ox/memory.db`
- **Mission harness (frozen)**: the tmux orchestrator from 0.x still runs via `ox go`

## How It Works

```
Claude Code desktop ── per-thread state files ──┐
GitHub (gh) ── your open PRs ───────────────────┼──▶ ox now ──▶ you, and every new thread (--brief)
yoke ── tasks ──────────────────────────────────┤
~/.ox/tracks.yaml ── workstreams ───────────────┘
```

ox reads the desktop app's thread state read-only. That format is internal to
the app, so ox treats every field as optional: if it changes, threads drop out
of `ox now` and nothing else breaks.

## Installation

```bash
go install github.com/ashvinbhat/ox/cmd/ox@latest
ox init
```

Requires [yoke](https://github.com/ashvinbhat/yoke) installed and initialized,
the [Claude Code](https://claude.com/claude-code) desktop app (macOS) for
`ox now`, and `gh` for PRs. tmux only for the frozen mission harness.

## Quick Start

```bash
ox init                          # Initialize ~/.ox
ox hooks init                    # Start every thread with ox now --brief
$EDITOR ~/.ox/tracks.yaml        # Name your workstreams
ox                               # Same as ox now
```

## Commands

### Where things stand
```bash
ox now                           # Threads by state and track, tasks, PRs without a thread
ox now --brief                   # The short version new threads get
ox now --track durable           # One track
ox now --json                    # For agents and scripts
ox worktrees prune --dry-run     # Worktrees whose PR merged (the hook prunes hourly)
```

`~/.ox/tracks.yaml`:
```yaml
tracks:
  - name: Durable agents
    short: durable               # column label; defaults to the first word
    goal: Agent turns survive restarts.
    match: [durable, redis, valkey]   # matched at a word start in titles, branches, repos
```

### Missions (frozen)
```bash
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

`ox hooks init` installs the `ox-brief` SessionStart hook into Claude Code's
native hook system (`~/.claude/settings.json`), so every new thread starts with
`ox now --brief`. It also deletes the pre-1.0 `ox-instructions` and
`yoke-ready-tasks` scripts.

## Directory Structure

```
~/.ox/
├── ox.yaml              # Config (repos, model tiers, memory.embeddings, budgets)
├── tracks.yaml          # Workstreams for ox now
├── repos/               # Registered codebases (base clones)
├── reviews/             # Review rounds per PR
├── worktrees/           # Git worktrees: <repo>/m<seq>-<agent>, m<seq>-integration
├── missions/            # Missions (frozen harness)
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
