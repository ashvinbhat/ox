# ox

> What your Claude Code threads draw on — built on [yoke](https://github.com/ashvinbhat/yoke)

You work in Claude Code desktop threads. ox keeps them pointed the same way:
every thread starts knowing what needs you, what's in review and what's
running, grouped by your tracks.

You never need to type an `ox` command. Ask a thread in your own words:

| You say | The thread runs |
|---|---|
| "what's waiting on me?" | `ox asks` |
| "where are things?" | `ox now` |
| "what did we decide this week?" | `ox journal --days 7` |
| "let me own this one" | the own-it round |
| "recap" | the weekly recap |
| "check my memory" | the memory check |
| "clean up my threads" | tidy |
| "any review asks?" / "review <PR>" | the review desk |
| "park this" / "I'll come back to this" | park: a resumption note, and the thread shows as parked |

The first thread each day also mentions, in one line, anything due: the
weekly recap or memory suggestions. The commands below are what threads use.

## Features

- **`ox now`**: every desktop thread by state — needs you, in review, working,
  stuck, done — tagged with its track, plus yoke tasks and open PRs no thread owns
- **Start brief**: a SessionStart hook gives each new thread `ox now --brief`
- **Board**: a side panel in every desktop thread listing the threads that
  need you and what each is waiting on, in sync across threads
- **Tracks**: your workstreams in `~/.ox/tracks.yaml`
- **Asks**: one ranked queue of what only you can do (thread questions, review
  asks, PRs ready to merge, memory, recap); PR fixes go to threads. `/tidy`
  archives, renames and groups threads after your OK
- **Learning loop**: threads log each decision (chosen, over what, why) to a
  daily journal; own-it rounds have you predict the approach before the build;
  a Friday recap asks you to explain three of the week's concepts back
- **Memory check**: a weekly audit of Claude's memory store (gone paths, code
  and commands, index drift, stale and stranded memories); `/memory-check`
  walks you through fixes and applies only what you approve
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
ox setup                         # Start hook, skills and board panel for every thread
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

### Reviews
Reviews start only when someone asks. In a thread:
```
/review-asks                     # Pick up review asks from Slack DMs and mentions
/review-pr <pr-url>              # Review against the whole codebase; re-review grades open findings
```
The commands those skills drive:
```bash
ox reviews                       # Queue: to review, re-review, waiting on author
ox review ask <pr-url> --from X  # Record an ask
ox review start <pr-url>         # Checkout at PR head, diff, interdiff, open findings
ox review post <pr-url> --file round.json --confirm   # Post an approved round
ox review record <pr-url> --file round.json           # Save a round without posting
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

### Asks
```bash
ox asks [--offline]      # What waits on you, ranked; PR fixes for threads
ox asks drop <id>        # Hide one until it changes (ids in --json)
```
The brief shows the top three. PR state is cached and refreshed in the
background once it's 10 minutes old. `/tidy` in a thread proposes archiving finished threads,
renaming duplicates and grouping by track, and applies what you approve.

### Parking
`/park` in a thread writes a resumption note as its last message (goal, done,
next, open, state) and records the next step:
```bash
ox park --next "Rebase on main and rerun the build"
```
`ox now` shows the thread as parked with that step until it's active again.
Parked threads are never asks and never archived.

### Journal
Threads log decisions as they make them; ox adds the time, thread and track.
```bash
ox journal [--days 7] [--track ox]   # Read: today, or the last N days
ox journal status                    # Counts, own-it rounds left, recap due
ox journal add --decided … --over … --why …
```
`/own-it` in a thread: you predict the approach, Claude builds, then compares
and explains each gap. Tracks with `own: true` in `tracks.yaml` get offered a
round (2 a day). `/recap`: three concepts from the week, explained back.

### Memory
Claude Code's auto-memory is the one store. `ox memory audit` checks it; the
`ox-brief` hook runs the audit weekly and the brief shows the count.
```bash
ox memory audit [--json]         # What may be stale, changed, duplicated or broken
ox memory audit --keep <id>      # Fine as is; never flag it again
ox memory recall "query"         # Search the memory.db archive
ox learnings [--tag x]           # Same archive, by tag
```
In a thread, `/memory-check` goes through the suggestions five at a time and
applies only the fixes you approve. A memory can cite what it depends on
(`metadata.cites: ["backend:SegmentService", "~/.ox/tracks.yaml"]`) so the
audit checks exactly that: gone, or changed since the memory was confirmed.

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
├── journal/             # Decision journal, one file a day
├── asks/                # PR state cache and dropped asks
├── worktrees/           # Git worktrees: <repo>/m<seq>-<agent>, m<seq>-integration
├── missions/            # Missions (frozen harness)
│   └── m<seq>-<slug>/   # mission.yaml, plan.md, events.jsonl, workers/, jobs/…
├── memory-check/        # Last memory audit and kept suggestions
├── memory.db            # Memory archive from before 1.0
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
