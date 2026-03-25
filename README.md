# ox

> Agent workspace manager - Built on yoke

Creates structured workspaces for AI-assisted development with context, personas, skills, and Claude Code hooks.

## Features

- **Task-driven**: Every session tied to a [yoke](https://github.com/ashvinbhat/yoke) task
- **Context-rich**: AGENTS.md with full task context, notes, dependencies
- **Persona-based**: Right mindset for the job (builder, explorer, reviewer, captain)
- **Skill-augmented**: Relevant expertise auto-injected based on tags
- **Hook-enabled**: Context injected at Claude Code session start

## How It Works

```
┌─────────────────────────────────────────────────────────────────┐
│  Terminal 1: ox CLI                                             │
│  $ ox pickup 9 --repos backend                                  │
│    → Creates workspace at ~/.ox/tasks/9-feature/                │
│    → Creates git worktree with branch ox/9-feature              │
│    → Generates AGENTS.md with task context + persona + skills   │
│    → Symlinks CLAUDE.md → AGENTS.md                             │
│                                                                 │
│  $ ox open                                                      │
│    → Opens workspace in IDE                                     │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│  Terminal 2: Claude Code (in workspace)                         │
│  $ cd ~/.ox/tasks/9-feature/ && claude                          │
│    → Claude Code reads CLAUDE.md automatically                  │
│    → Has full task context, skills, related files               │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│  Back to Terminal 1                                             │
│  $ ox ship                    # Push branch, create PR          │
│  $ ox done                    # Cleanup workspace, mark done    │
└─────────────────────────────────────────────────────────────────┘
```

## Installation

```bash
go install github.com/ashvinbhat/ox/cmd/ox@latest
ox init
```

Requires: [yoke](https://github.com/ashvinbhat/yoke) installed and initialized.

## Quick Start

```bash
# Initialize ox
ox init

# Register a codebase
ox repo add git@github.com:company/backend.git --name backend

# Pick up a yoke task
ox pickup 9 --repos backend

# Open in IDE
ox open

# Work with Claude Code in the workspace...

# Create PR when ready
ox ship

# Complete and cleanup
ox done
```

## Commands

### Workspace
```bash
ox pickup <id> --repos <repo>    # Create workspace for task
ox status                        # Current task and workspace
ox open                          # Open workspace in IDE
ox ship                          # Push and create PR
ox done [id]                     # Complete task, cleanup
```

### Repos
```bash
ox repo add <url> [--name x]     # Register a codebase
ox repo list                     # Show registered repos
ox repo remove <name>            # Unregister repo
```

### Personas
```bash
ox personas                      # List available personas
ox pickup --persona explorer     # Start with specific persona
ox morph <persona>               # Switch persona mid-task
```

### Skills
```bash
ox skill list                    # List available skills
ox skill inject <name>           # Add skill to workspace
ox skill eject <name>            # Remove skill from workspace
```

### Hooks
```bash
ox hooks                         # List hooks
ox hooks init                    # Initialize and install to Claude Code
ox hooks run <name>              # Test a hook
```

## Personas

| Persona | Role | Auto-triggers |
|---------|------|---------------|
| **captain** | Orchestrates, plans, delegates | `epic`, `project`, `plan`, `design` |
| **builder** | Implements, ships code | `bug`, `feature`, `fix`, `implement` |
| **explorer** | Researches, investigates | `research`, `spike`, `investigate` |
| **reviewer** | Reviews, checks quality | `review`, `pr`, `audit`, `quality` |

Personas are auto-selected based on task tags, or specified with `--persona`.

## Skills

Skills are expertise modules auto-injected based on task tags, persona, or task type:

```yaml
# ~/.ox/skills/skills.yaml
skills:
  backend-engineer:
    file: backend-engineer.md
    tags: [backend, java, python]
    personas: [builder]
  debugging:
    file: debugging.md
    tags: [bug, incident]
    personas: [builder, explorer]
```

## Hooks

Hooks inject context into Claude Code sessions:

- **yoke-ready-tasks**: Show ready tasks at session start
- **ox-instructions**: ox CLI quick reference
- **workspace-context**: Current task summary

Run `ox hooks init` to install hooks to Claude Code.

## Directory Structure

```
~/.ox/
├── ox.yaml              # Configuration
├── repos/               # Registered codebases (cloned)
├── tasks/               # Active task workspaces
│   └── 9-feature/       # Workspace directory
│       ├── AGENTS.md    # Generated context
│       ├── CLAUDE.md    # Symlink → AGENTS.md
│       └── backend/     # Symlink → worktree
├── worktrees/           # Git worktrees
├── skills/              # Skill definitions
├── personas/            # Persona definitions
└── hooks/               # Claude Code hooks
```

## Configuration

```yaml
# ~/.ox/ox.yaml
agent: claude
ide: windsurf

repos:
  backend:
    url: git@github.com:company/backend.git
    base_branch: main
    copy_files: [.env, .vscode]
    post_setup: npm install

defaults:
  persona: builder
```

## Status

**Implemented:**
- Phase O0: Foundation (repos, pickup, done)
- Phase O1: Git Integration (worktrees, ship)
- Phase O2: Rich Context Generation (AGENTS.md)
- Phase O3: Skills System (auto-injection)
- Phase O4: Personas (auto-selection)
- Phase O5: Hook System (Claude Code integration)

**Coming:**
- Phase O6: Checkpoints & Memory
- Phase O7: Learning System

See [ROADMAP.md](./ROADMAP.md) for full plan.

## Dependencies

- [yoke](https://github.com/ashvinbhat/yoke) - Task management
- Git with worktree support
- Claude Code (optional, for hooks)
