# Ox Roadmap

> Agent workspace manager - Built on yoke

## Vision

Make AI-assisted development seamless by providing structured workspaces, relevant context, and appropriate personas for every task. Inspired by [jeff](https://github.com/NeerajG03/JEFF) but with native yoke integration.

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
└── memory.db                 # SQLite for cross-session memory
```

## Dependency

**ox requires yoke** - Task management handled by yoke. Ox imports yoke as a Go library for direct access to:
- Task data (title, status, priority, tags, body)
- Notes and event history
- Parent/child relationships and blockers
- Notion sync metadata

```go
import "github.com/ashvinbhat/yoke/internal/task"

store, _ := task.NewStore("~/.yoke/yoke.db")
t, _ := store.Get("9")
notes, _ := store.GetNotes(t.ID)
```

---

## Phase O0: Foundation ✅ READY

**Goal:** Basic workspace creation and repo management

### Deliverables
- [ ] `ox init` - Initialize ~/.ox structure
- [ ] `ox repo add <url> [--name x]` - Register and clone codebase
- [ ] `ox repo list` - Show registered repos
- [ ] `ox repo remove <name>` - Unregister repo
- [ ] `ox pickup <task-id> --repos <name>` - Create workspace with worktree
- [ ] `ox status` - Show current task/workspace
- [ ] `ox done [task-id]` - Complete task, cleanup workspace
- [ ] Basic CLAUDE.md generation

### Config (ox.yaml)
```yaml
agent: claude
ide: cursor

repos:
  backend:
    url: git@github.com:company/backend.git
    base_branch: main
  frontend:
    url: git@github.com:company/frontend.git

defaults:
  persona: builder
```

### Exit Criteria
- Can register repos via CLI
- Can create workspace from yoke task
- Worktree created and symlinked
- Basic CLAUDE.md generated
- Can complete and cleanup

---

## Phase O1: Git Integration

**Goal:** Full worktree and branch lifecycle

### Deliverables
- [ ] Auto-create branch: `ox/<task-id>-<slug>`
- [ ] `ox worktree list` - Show all worktrees
- [ ] `ox worktree add <repo>` - Add worktree to current task
- [ ] `ox worktree rm <repo>` - Remove worktree
- [ ] `ox ship [--repo x] [--draft]` - Push and create PR
- [ ] `ox open [task-id]` - Open workspace in IDE
- [ ] Cleanup worktrees on `ox done`

### Exit Criteria
- Never manually create branches
- PRs created via `ox ship`
- Clean worktree lifecycle

---

## Phase O2: Rich Context Generation

**Goal:** CLAUDE.md with full yoke context

### Deliverables
- [ ] Task title, body, status, priority
- [ ] All notes from yoke
- [ ] Blockers and dependencies
- [ ] Parent/child task hierarchy
- [ ] Event history (recent activity)
- [ ] Notion link if available
- [ ] Custom template support

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

## External
Notion: https://notion.so/...

---
[Persona section]
---
[Skills section]
```

### Exit Criteria
- AI has complete task context
- Notes and history visible
- Dependencies clear

---

## Phase O3: Skills System

**Goal:** Auto-inject relevant expertise

### Deliverables
- [ ] `ox skill list` - Available skills
- [ ] `ox skill add <path>` - Register skill
- [ ] `ox skill remove <name>` - Unregister
- [ ] `ox skill inject <name>` - Add to current workspace
- [ ] `ox skill eject <name>` - Remove from workspace
- [ ] Auto-inject based on task tags
- [ ] Symlink-based (no copying)

### Skill Structure
```
~/.ox/skills/
├── go.md           # Go best practices
├── java.md         # Java/Spring patterns
├── testing.md      # Testing methodology
├── debugging.md    # Debug approach
└── git.md          # Git workflow
```

### Skill Mapping (ox.yaml)
```yaml
skill_tags:
  backend: [java, testing, debugging]
  frontend: [typescript, react]
  go: [go, testing]
  bug: [debugging]
```

### Exit Criteria
- Skills auto-inject from tags
- Manual inject/eject works
- CLAUDE.md includes skill content

---

## Phase O4: Personas

**Goal:** Right mindset for the task

### Deliverables
- [ ] `ox pickup --persona <name>` - Start with persona
- [ ] `ox morph <persona>` - Switch mid-task
- [ ] `ox personas` - List available
- [ ] Auto-select from task tags
- [ ] Persona in CLAUDE.md

### Personas

| Persona | Role | Auto-triggers |
|---------|------|---------------|
| **captain** | Orchestrates, plans, delegates | `epic`, `project` |
| **builder** | Implements, ships code | `bug`, `feature` |
| **explorer** | Researches, investigates | `research`, `spike` |
| **reviewer** | Reviews, checks quality | `review`, `pr` |

### Captain Persona (Orchestrator)
```markdown
# Captain Persona

You are the Captain — an orchestrator.

## Role
- Break down complex tasks into steps
- Decide which mode is best for each step
- Maintain big picture while delegating

## Workflow
1. Analyze task, create plan
2. For each step:
   - Handle planning/coordination yourself
   - Instruct: "Run `ox morph builder` for implementation"
   - Instruct: "Run `ox morph reviewer` for review"
3. Integrate and ship
```

### Exit Criteria
- Personas change AI behavior
- Captain can orchestrate workflow
- Auto-selection works

---

## Phase O5: Checkpoints & Memory

**Goal:** Survive context resets

### Deliverables
- [ ] `ox checkpoint --done "x" --next "y"` - Save progress
- [ ] `ox resume` - Restore from checkpoint
- [ ] `ox checkpoints` - List checkpoints
- [ ] Auto-checkpoint on `ox done`
- [ ] Checkpoint syncs to yoke notes
- [ ] SQLite memory for patterns

### Checkpoint Structure
```yaml
task_id: "9"
created_at: "2024-03-25T15:30:00Z"
done: "Implemented auth flow"
next: "Add unit tests"
files_changed:
  - internal/auth/handler.go
decisions:
  - "Using JWT with RS256"
blockers: []
```

### Exit Criteria
- Checkpoints persist across sessions
- Context survives compaction
- Decisions preserved

---

## Phase O6: Learning System

**Goal:** Get better over time

### Deliverables
- [ ] `ox learn "insight"` - Capture learning
- [ ] `ox learnings [--tag x]` - View learnings
- [ ] Auto-capture on task completion
- [ ] Surface relevant learnings on pickup
- [ ] Pattern detection

### Exit Criteria
- Learnings captured
- Relevant insights shown
- Actually improving

---

## Phase O7: Multi-Agent Support 💡 FUTURE

**Goal:** Multiple agents collaborate on complex tasks

### Why Consider Multi-Agent?

| Approach | Pros | Cons |
|----------|------|------|
| **Single agent + personas** | Simple, shared context, easy debug | Sequential, single perspective |
| **True multi-agent** | Parallel work, independent review, catches more errors | Complex coordination, context sync |

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

### Coordination via Shared Workspace
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

## Phase O8: Advanced Features 💡 FUTURE

### Scheduling
- [ ] `ox daily` - Morning briefing
- [ ] Deadline awareness from yoke
- [ ] `ox remind <task> <time>`
- [ ] Overdue alerts

### Integrations
- [ ] VS Code extension
- [ ] TUI dashboard
- [ ] Slack notifications
- [ ] Custom agent providers (Ollama, etc.)

### Team Features
- [ ] Shared skill libraries
- [ ] Team learnings
- [ ] Handoff between team members

---

## Technical Decisions

### Why Go?
- Import yoke as library (no shell-out)
- Single binary distribution
- Fast compilation

### Config Format
- YAML (matches yoke, human-friendly)
- JSON schema for editor support

### Storage
- Workspaces: filesystem
- Config: ox.yaml
- Memory/Learnings: SQLite

### AI Integration
- Primary: Claude Code CLI
- Future: Abstract provider interface

---

## Comparison: Ox vs Jeff vs Jarvis

| Aspect | Jarvis (old) | Jeff | Ox |
|--------|--------------|------|-----|
| Task system | Beads (external) | gig (external CLI) | yoke (Go library) |
| Repo management | Manual + symlinks | `jeff repo add` | `ox repo add` |
| Config | Scattered YAML | Single JSON | Single YAML |
| Skills | Deep hierarchy | Flat .skills/ | Flat skills/ |
| Context | Static CLAUDE.md | Generated | Generated + yoke data |
| Multi-agent | No | No (personas only) | Personas + optional spawn |

### Ox's Unique Value
1. **Native yoke integration** - Full access to task data, notes, history
2. **Richer context** - Blockers, dependencies, Notion links in CLAUDE.md
3. **Checkpoint → yoke** - Progress saved as yoke notes
4. **Learning feedback** - Insights improve future tasks

---

## Status Key

- ✅ READY - Can start now
- 🔜 NEXT - Coming up
- 📋 PLANNED - On roadmap
- 💡 FUTURE - Not committed
