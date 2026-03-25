# Ox Roadmap

> Agent workspace manager - Built on yoke

## Vision

Make AI-assisted development seamless by providing structured workspaces, relevant context, and appropriate personas for every task.

## Architecture

```
$OX_HOME/                     (~/.ox)
├── ox.yaml                   # Config (repos, defaults, skill mappings)
├── repos/                    # Registered codebases (cloned via ox repo add)
│   ├── backend/
│   ├── frontend/
│   └── jarvis/
├── tasks/                    # Active task workspaces
│   └── 9-refactor-linkedin/
│       ├── CLAUDE.md         # Generated (persona + task + skills)
│       ├── backend/          # Symlink → worktrees/backend/9
│       └── state.yaml        # Checkpoint state
├── worktrees/                # Git worktrees (managed by ox)
│   └── backend/
│       └── 9/                # Branch: ox/9-refactor-linkedin
├── skills/                   # Flat skill files (SKILL.md)
│   ├── go.md
│   ├── java.md
│   ├── testing.md
│   └── debugging.md
├── personas/                 # Persona definitions
│   ├── captain.md            # Orchestrator
│   ├── builder.md            # Implementer
│   ├── explorer.md           # Researcher
│   └── reviewer.md           # Quality checker
├── templates/                # Task templates
│   ├── bug-fix.yaml
│   ├── feature.yaml
│   └── refactor.yaml
└── memory.db                 # SQLite for cross-session memory
```

## Dependency

**ox requires yoke** - Task management handled by yoke. Ox imports yoke as a Go library for direct access to:
- Task data (title, status, priority, tags, body)
- Notes and event history
- Parent/child relationships and blockers
- Notion sync metadata

```go
import "github.com/ashvinbhat/yoke/task"

store, _ := task.NewStore("~/.yoke/yoke.db")
t, _ := store.Get("9")
notes, _ := store.GetNotes(t.ID)
```

---

## Key Features

1. **Native yoke integration** - Full access to task data, notes, history
2. **Notion sync** - Bidirectional sync via yoke
3. **Declarative setup** - `copy_files`, `post_setup` in config
4. **Checkpoint → yoke** - Progress saved as yoke notes
5. **Learning system** - Insights improve future tasks
6. **Quality gates** - Pre-ship checks and auto PR descriptions
7. **Task templates** - Consistent workflows for task types
8. **Daily workflow** - Briefings, standups, focus mode

---

## Phase O0: Foundation ✅ DONE

**Goal:** Basic workspace creation and repo management

### Deliverables
- [x] `ox init` - Initialize ~/.ox structure
- [x] `ox repo add <url> [--name x]` - Register and clone codebase
- [x] `ox repo list` - Show registered repos
- [x] `ox repo remove <name>` - Unregister repo
- [x] `ox pickup <task-id> --repos <name>` - Create workspace with worktree
- [x] `ox status` - Show current task/workspace
- [x] `ox done [task-id]` - Complete task, cleanup workspace
- [x] Basic CLAUDE.md generation
- [x] `ox tasks` - List yoke tasks
- [x] `ox task <id>` - Show yoke task details
- [x] `copy_files` - Copy .env, .vscode etc to worktrees
- [x] `post_setup` - Run setup commands after worktree creation

### Config (ox.yaml)
```yaml
agent: claude
ide: windsurf

repos:
  backend:
    url: git@github.com:company/backend.git
    base_branch: main
    copy_files: [.env, .vscode]
  frontend:
    url: git@github.com:company/frontend.git
    copy_files: [.env]
    post_setup: npm install

defaults:
  persona: builder
```

### Exit Criteria ✅
- Can register repos via CLI
- Can create workspace from yoke task
- Worktree created and symlinked
- Files copied, post-setup runs
- Basic CLAUDE.md generated
- Can complete and cleanup

---

## Phase O1: Git Integration ✅ DONE

**Goal:** Full worktree and branch lifecycle

### Deliverables
- [x] Auto-create branch: `ox/<task-id>-<slug>`
- [x] `ox worktree list` - Show all worktrees
- [x] `ox worktree add <repo>` - Add worktree to current task
- [x] `ox worktree rm <repo>` - Remove worktree
- [x] `ox ship [--repo x] [--draft]` - Push and create PR
- [x] `ox open [task-id]` - Open workspace in IDE
- [x] Cleanup worktrees on `ox done`

### Exit Criteria ✅
- Never manually create branches
- PRs created via `ox ship`
- Clean worktree lifecycle

---

## Phase O2: Rich Context Generation ✅ DONE

**Goal:** AGENTS.md with full yoke context (CLAUDE.md symlinks to it)

### Deliverables
- [x] Task title, body, status, priority
- [x] All notes from yoke
- [x] Blockers and dependencies (resolved with titles)
- [x] Parent/child task hierarchy
- [x] Event history (recent activity)
- [x] Notion link if available
- [ ] Custom template support
- [x] Related files detection (from git history)

### CLAUDE.md Structure
```markdown
# Task #9: Refactor LinkedIn workflow

## Status
IN PROGRESS | P4 | Tags: backend, refactor

## Context
[Task body from yoke]

## Notes
- 2024-03-25: Started investigation
- 2024-03-24: Linked from Notion

## Dependencies
Blocked by: (none)
Blocking: (none)
Parent: (none)

## Recent Activity
- status: pending → in_progress
- linked to Notion

## Related Files (from git history)
- internal/linkedin/handler.go (12 commits)
- internal/linkedin/service.go (8 commits)

## External
Notion: https://notion.so/...

---
[Persona section]
---
[Skills section]
```

### Exit Criteria ✅
- AI has complete task context
- Notes and history visible
- Dependencies clear

---

## Phase O3: Skills System ✅ DONE

**Goal:** Auto-inject relevant expertise based on task attributes

### Deliverables
- [x] `ox skill list` - Available skills
- [x] `ox skill add <path>` - Register skill with metadata
- [x] `ox skill remove <name>` - Unregister
- [x] `ox skill inject <name>` - Add to current workspace
- [x] `ox skill eject <name>` - Remove from workspace
- [x] **3D Auto-injection**:
  - Match by persona (builder gets testing.md)
  - Match by tags (backend gets java.md)
  - Match by task type (bug gets debugging.md)
- [x] Symlink-based (no copying)
- [x] `skills.yaml` registry with metadata

### Skill Registry (skills.yaml)
```yaml
skills:
  java:
    file: java.md
    tags: [backend, java]
    personas: [builder]
  debugging:
    file: debugging.md
    tags: [bug, incident]
    personas: [builder, explorer]
  testing:
    file: testing.md
    tags: [test, qa]
    personas: [builder, reviewer]
```

### Exit Criteria ✅
- Skills auto-inject from tags/persona/type
- Manual inject/eject works
- AGENTS.md includes skill content

---

## Phase O4: Personas ✅ DONE

**Goal:** Right mindset for the task

### Deliverables
- [x] `ox pickup --persona <name>` - Start with persona
- [x] `ox morph <persona>` - Switch mid-task (regenerates AGENTS.md)
- [x] `ox personas` - List available with metadata
- [x] Auto-select from task tags
- [x] Persona in AGENTS.md with role and content

### Personas

| Persona | Role | Auto-triggers |
|---------|------|---------------|
| **captain** | Orchestrates, plans, delegates | `epic`, `project`, `plan`, `design` |
| **builder** | Implements, ships code | `bug`, `feature`, `fix`, `implement` |
| **explorer** | Researches, investigates | `research`, `spike`, `investigate`, `explore` |
| **reviewer** | Reviews, checks quality | `review`, `pr`, `audit`, `quality` |

### Exit Criteria ✅
- Personas change AI behavior
- Captain can orchestrate workflow
- Auto-selection works

---

## Phase O5: Hook System ✅ DONE

**Goal:** Inject context at agent session start

### Deliverables
- [x] Hook scripts in `~/.ox/hooks/`
- [x] Built-in hooks:
  - `yoke-ready-tasks` - Show ready tasks at session start
  - `ox-instructions` - ox CLI reference
  - `workspace-context` - Current task summary
- [x] Custom hooks support (add .sh scripts to ~/.ox/hooks/)
- [x] Claude Code integration (`~/.claude/settings.json`)
- [x] `ox hooks init` - Initialize and install hooks
- [x] `ox hooks` - List available hooks
- [x] `ox hooks run <name>` - Test hook output

### Commands
```bash
ox hooks             # List available hooks
ox hooks init        # Create scripts and install to Claude Code
ox hooks run <name>  # Test a hook manually
```

### Hook Script Format
```bash
#!/bin/bash
# Output JSON for Claude Code
INPUT=$(cat)
CONTEXT="Ready tasks: $(yoke ready --short)"
jq -n --arg ctx "$CONTEXT" '{
  hookSpecificOutput: {
    hookEventName: "SessionStart",
    additionalContext: $ctx
  }
}'
```

### Exit Criteria ✅
- Context injected at session start
- AI knows ready tasks without asking
- Custom hooks work

---

## Phase O6: Checkpoints & Memory 🔜 NEXT

**Goal:** Survive context resets

### Deliverables
- [ ] `ox checkpoint [--done "x"] [--next "y"]` - Save progress
- [ ] `ox resume` - Restore from checkpoint
- [ ] `ox checkpoints` - List checkpoints
- [ ] Auto-checkpoint on `ox done`
- [ ] Checkpoint syncs to yoke notes
- [ ] SQLite memory for patterns
- [ ] Track files changed since last checkpoint

### Checkpoint Structure
```yaml
task_id: "9"
created_at: "2024-03-25T15:30:00Z"
done: "Implemented auth flow"
next: "Add unit tests"
files_changed:
  - internal/auth/handler.go
  - internal/auth/handler_test.go
decisions:
  - "Using JWT with RS256"
  - "Storing refresh tokens in Redis"
blockers: []
```

### Exit Criteria
- Checkpoints persist across sessions
- Context survives compaction
- Decisions preserved
- Synced to yoke notes

---

## Phase O7: Learning System 📋 PLANNED

**Goal:** Get better over time

### Deliverables
- [ ] `ox learn "insight"` - Capture learning
- [ ] `ox learnings [--tag x]` - View learnings
- [ ] Auto-capture on task completion (outcome)
- [ ] Surface relevant learnings on pickup
- [ ] Pattern detection (what works for which task types)
- [ ] Store in yoke task outcome field

### Learning Categories
- **Approaches that worked** - "Using feature flags for gradual rollout"
- **Gotchas discovered** - "MongoDB aggregations need index hints"
- **Time estimates** - "Auth features take 2x estimated time"
- **Tool preferences** - "Use REST Assured for API tests"

### Exit Criteria
- Learnings captured
- Relevant insights shown on pickup
- Actually improving task execution

---

## Phase O8: Pre-Ship Quality Gates 💡 NEW

**Goal:** Ensure quality before shipping

### Deliverables
- [ ] `ox precheck` - Run quality checks before ship
- [ ] Configurable checks per repo:
  - Run tests
  - Run linter
  - Check for TODO/FIXME
  - Verify no console.log/print statements
  - Check test coverage threshold
- [ ] `ox ship` runs prechecks by default
- [ ] `ox ship --skip-checks` to bypass
- [ ] Auto-generate PR description from:
  - Checkpoint history
  - Files changed
  - Test results

### Config
```yaml
repos:
  backend:
    prechecks:
      - command: mvn test
        name: Unit Tests
      - command: mvn checkstyle:check
        name: Checkstyle
    pr_template: |
      ## Summary
      {{.TaskTitle}}

      ## Changes
      {{range .FilesChanged}}- {{.}}
      {{end}}

      ## Test Results
      {{.TestOutput}}
```

### Exit Criteria
- No shipping without passing checks
- PR descriptions are useful
- Quality improves over time

---

## Phase O9: Quick Task Switching 💡 NEW

**Goal:** Rapidly switch between tasks without full teardown

### Deliverables
- [ ] `ox switch <task-id>` - Switch to another task
- [ ] `ox pause` - Pause current task (auto-checkpoint)
- [ ] `ox continue` - Resume paused task
- [ ] Keep worktrees around for quick switch
- [ ] Memory of which repos each task uses
- [ ] `ox recent` - List recently worked tasks

### Workflow
```bash
ox pickup 9 --repos backend    # Start task 9
# ... work on task 9 ...
ox switch 12 --repos frontend  # Switch to task 12 (pauses 9)
# ... work on task 12 ...
ox continue 9                   # Back to task 9 (already set up!)
```

### Exit Criteria
- Switch takes <5 seconds
- No lost context
- Worktrees reused

---

## Phase O10: Task Templates 💡 NEW

**Goal:** Predefined structures for common task types

### Deliverables
- [ ] `ox template list` - Available templates
- [ ] `ox template use <name>` - Apply template to pickup
- [ ] Templates define:
  - Default repos
  - Default persona
  - Default skills
  - Checklist items
  - Suggested workflow

### Built-in Templates

**bug-fix.yaml:**
```yaml
name: Bug Fix
persona: builder
skills: [debugging, testing]
checklist:
  - Reproduce the bug
  - Write failing test
  - Fix the issue
  - Verify test passes
  - Check for regressions
```

**feature.yaml:**
```yaml
name: Feature
persona: builder
skills: [testing]
checklist:
  - Review requirements
  - Design approach
  - Implement core logic
  - Add tests
  - Update documentation
```

**spike.yaml:**
```yaml
name: Research Spike
persona: explorer
skills: [research]
checklist:
  - Define questions to answer
  - Research options
  - Document findings
  - Make recommendation
```

### Exit Criteria
- Templates speed up pickup
- Consistent workflows
- Checklists guide work

---

## Phase O11: Daily Workflow 💡 NEW

**Goal:** Productivity features for daily work

### Deliverables
- [ ] `ox daily` - Morning briefing:
  - In-progress tasks
  - Blocked tasks
  - Ready tasks by priority
  - PRs awaiting review
  - Overdue tasks
- [ ] `ox focus <task-id>` - Focus mode (hide other tasks)
- [ ] `ox standup` - Generate standup summary:
  - What I did yesterday
  - What I'm doing today
  - Blockers
- [ ] Time tracking (optional):
  - `ox start` / `ox stop` timestamps
  - Daily/weekly reports

### Exit Criteria
- Clear daily priorities
- Easy standup generation
- Optional time insights

---

## Phase O12: Multi-Agent Support 💡 FUTURE

**Goal:** Multiple agents collaborate on complex tasks

### When Multi-Agent Helps
- **Complex refactors**: Explorer researches while Builder prototypes
- **Critical changes**: Independent review from separate agent
- **Large tasks**: Parallel work on different components

### Proposed Design
```bash
# Default: single agent with personas
ox pickup 9 --persona captain

# Optional: spawn background agent
ox spawn explorer "research the auth flow" --background
ox spawn reviewer "review my changes" --wait

# Or: full multi-agent mode
ox pickup 9 --multi-agent
# Captain spawns builder + reviewer automatically
```

### Coordination
```
tasks/9-feature/
├── CLAUDE.md           # Shared context
├── .agents/
│   ├── captain.log     # Captain's decisions
│   ├── builder.log     # Builder's progress
│   └── reviewer.log    # Reviewer's feedback
└── handoff.yaml        # Inter-agent communication
```

### Exit Criteria
- Can spawn parallel agents
- Agents coordinate via workspace
- Quality improves on complex tasks

---

## Phase O13: Integrations 💡 FUTURE

### GitHub Integration
- [ ] `ox gh-issue <url>` - Create yoke task from GitHub issue
- [ ] Auto-link PRs to issues
- [ ] Sync issue comments to yoke notes

### Slack Integration
- [ ] Notify on task completion
- [ ] Notify on PR creation
- [ ] Daily summary to channel
- [ ] `/ox status` slash command

### CI/CD Awareness
- [ ] Track build status for branches
- [ ] Notify on build failure
- [ ] `ox ci-status` - Show CI status for current task

### IDE Extensions
- [ ] VS Code extension
- [ ] Cursor extension
- [ ] Windsurf extension
- [ ] Show task context in sidebar

---

## Phase O14: Team Features 💡 FUTURE

### Deliverables
- [ ] Shared skill libraries (Git repo)
- [ ] Team learnings database
- [ ] Task handoff:
  - `ox handoff <user>` - Hand task to teammate
  - Include full context, checkpoints, decisions
- [ ] Code review queue:
  - `ox review-queue` - Tasks awaiting review
  - `ox claim-review <task>` - Take a review

### Exit Criteria
- Teams share knowledge
- Smooth handoffs
- Review workflow integrated

---

## Technical Decisions

### Why Go?
- Import yoke as library (no shell-out)
- Single binary distribution
- Fast compilation

### Config Format
- YAML (matches yoke, human-friendly)
- Consider JSON schema for editor support

### Storage
- Workspaces: filesystem
- Config: ox.yaml
- Memory/Learnings: SQLite (or yoke DB)

### AI Integration
- Primary: Claude Code CLI
- Future: Abstract provider interface (Cursor, Windsurf, etc.)

---

## Status Key

- ✅ DONE - Complete
- 🔜 NEXT - Coming up
- 📋 PLANNED - On roadmap
- 💡 NEW - Added to roadmap
- 💡 FUTURE - Not committed yet
