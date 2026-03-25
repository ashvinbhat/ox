# Ox - Agent Instructions

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
- `ox tasks` - lists yoke tasks
- `ox repo list` - lists registered repos
- `ox skill list` - lists available skills
- `ox personas` - lists personas
- `ox hooks` - lists hooks
- `ox learn` - works anywhere (but adds workspace context if in one)
- `ox learnings` - lists all learnings

## Yoke Integration

ox depends on yoke for task management. Key points:

- yoke binary location: `~/go/bin/yoke`
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
├── ox.yaml              # Config
├── repos/               # Cloned repos (base)
├── worktrees/           # Git worktrees per task
├── tasks/               # Workspace directories
│   └── <task-id>-<slug>/
│       ├── AGENTS.md    # Generated context
│       ├── CLAUDE.md    # Symlink → AGENTS.md
│       └── <repo>/      # Symlink → worktree
├── skills/              # Skill markdown files
├── personas/            # Persona definitions
├── hooks/               # Claude Code hooks
└── learnings.db         # SQLite learnings store
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
