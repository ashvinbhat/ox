# ox

> The ox that pulls your workload.

An agent workspace manager built on [yoke](https://github.com/ashvinbhat/yoke). Gives AI agents structured workspaces, personas, skills, and lifecycle management for task-driven development.

## Philosophy

- **Task-driven**: Every work session tied to a yoke task
- **Context-rich**: AI always has full context (CLAUDE.md)
- **Persona-based**: Right mindset for the job
- **Skill-augmented**: Relevant expertise auto-injected
- **Checkpoint-able**: Survive context resets

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                           YOKE                                  │
│                    (Task management)                            │
└─────────────────────────────┬───────────────────────────────────┘
                              │ uses
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                            OX                                   │
│                  (Agent workspace manager)                      │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                     SUPERVISOR                            │  │
│  │  • Routes to personas    • Manages lifecycle              │  │
│  │  • Schedules work        • Tracks progress                │  │
│  └──────────────────────────────────────────────────────────┘  │
│                              │                                  │
│         ┌────────────────────┼────────────────────┐            │
│         ▼                    ▼                    ▼            │
│  ┌────────────┐       ┌────────────┐       ┌────────────┐     │
│  │  BUILDER   │       │  EXPLORER  │       │  REVIEWER  │     │
│  │  (jock)    │       │  (nerd)    │       │  (scout)   │     │
│  └────────────┘       └────────────┘       └────────────┘     │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                   SHARED SERVICES                         │  │
│  │  Workspace │ Skills │ Memory │ Context Gen │ Learning     │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## Installation

```bash
go install github.com/ashvinbhat/ox/cmd/ox@latest
```

Requires: [yoke](https://github.com/ashvinbhat/yoke) installed and initialized.

## Quick Start

```bash
# Pick up a task (creates workspace)
ox pickup <task-id>

# Check status
ox status

# Work with AI
ox work

# Save progress
ox checkpoint "implemented auth flow"

# Create PR
ox ship

# Complete
ox done
```

## Commands

### Workspace
```bash
ox pickup <task-id>           # Create workspace, branch, inject context
ox status                     # Current task and workspace status
ox checkpoint "msg"           # Save progress
ox resume                     # Resume from checkpoint
ox ship                       # Create PR from workspace
ox done                       # Complete task, cleanup workspace
```

### Personas
```bash
ox pickup --as builder        # Work as builder persona
ox morph explorer             # Switch persona mid-task
ox personas                   # List available personas
```

### Skills
```bash
ox skills                     # List available skills
ox inject <skill>             # Add skill to current workspace
ox eject <skill>              # Remove skill from workspace
```

### AI Integration
```bash
ox work                       # Launch Claude Code with context
ox ask "question"             # Quick question about current task
```

## Personas

| Persona | Focus | Auto-triggers |
|---------|-------|---------------|
| **builder** | Implementation, shipping code | `bug`, `feature`, `impl` |
| **explorer** | Research, investigation | `research`, `spike`, `rfc` |
| **reviewer** | Code review, quality | `review`, `pr` |
| **planner** | Architecture, planning | `design`, `plan`, `arch` |

## Skills

Skills are expertise modules auto-injected based on task tags:

```
~/.ox/skills/
├── git/
│   └── SKILL.md
├── testing/
│   └── SKILL.md
├── debugging/
│   └── SKILL.md
└── ...
```

## Storage

```
~/.ox/
├── config.yaml          # Configuration
├── personas/            # Persona definitions
├── skills/              # Skill modules
├── memory/              # Cross-session memory
└── checkpoints/         # Saved progress
```

## Configuration

```yaml
# ~/.ox/config.yaml
workspace:
  base_dir: "~/workspaces"   # Where to create workspaces
  use_worktree: true         # Use git worktrees

ai:
  provider: "claude"         # claude, ollama, etc.
  model: "claude-sonnet"     # Model to use

defaults:
  persona: "builder"         # Default persona
```

## Status

Under development. See [ROADMAP.md](./ROADMAP.md) for plan.

Depends on [yoke](https://github.com/ashvinbhat/yoke) for task management.
