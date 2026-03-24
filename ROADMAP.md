# Ox Roadmap

> Agent workspace manager - Built on yoke

## Vision

Make AI-assisted development seamless by providing structured workspaces, relevant context, and appropriate personas for every task.

## Dependency

**ox requires yoke** - Task management is handled by yoke. Ox focuses on:
- Workspace creation and management
- Context generation (CLAUDE.md)
- Persona loading
- Skill injection
- Memory and checkpoints
- AI integration

```
yoke (tasks) ──────▶ ox (workspaces + AI)
```

---

## Phase O0: Basic Workspace 📋 WAITING ON YOKE

**Goal:** Create workspace for a task

### Deliverables
- [ ] `ox pickup <task-id>` - Create workspace for yoke task
- [ ] `ox status` - Show current task/workspace
- [ ] `ox done` - Complete task (calls yoke done)
- [ ] Basic CLAUDE.md generation with task details

### Workspace Structure
```
~/workspaces/<task-id>/
├── CLAUDE.md            # Generated context
└── (git worktree or clone)
```

### Exit Criteria
- Can create workspace from yoke task
- CLAUDE.md has task context
- Can complete task from ox

---

## Phase O1: Git Integration

**Goal:** Automatic branch management

### Deliverables
- [ ] Auto-create branch on `ox pickup`
- [ ] Branch naming: `ox/<id>-<slug>`
- [ ] Git worktree support (isolated directory)
- [ ] `ox ship` - Create PR from workspace
- [ ] Return to main on `ox done`

### Exit Criteria
- Never manually create task branches
- PRs created automatically
- Clean branch management

---

## Phase O2: Context Generation

**Goal:** Rich CLAUDE.md for AI

### Deliverables
- [ ] Include task title, body, status
- [ ] Include task notes from yoke
- [ ] Include related/blocked tasks
- [ ] Include recent git history
- [ ] Custom templates support

### CLAUDE.md Structure
```markdown
# Task: <title>

## Context
<task body>

## Status
<current status, blockers>

## Notes
<notes from yoke>

## Related Tasks
<parent, children, blockers>

## Recent Activity
<recent commits, changes>

## Skills
<injected skills below>
```

### Exit Criteria
- AI has full task context
- Context is accurate and useful

---

## Phase O3: Skills System

**Goal:** Inject relevant expertise

### Deliverables
- [ ] Skill directory structure
- [ ] `ox skills` - List available skills
- [ ] `ox inject <skill>` - Add to workspace
- [ ] `ox eject <skill>` - Remove from workspace
- [ ] Auto-inject based on task tags
- [ ] Symlink-based (no copying)

### Skill Structure
```
~/.ox/skills/
├── git/
│   └── SKILL.md         # Git workflow expertise
├── testing/
│   └── SKILL.md         # Testing patterns
├── debugging/
│   └── SKILL.md         # Debug methodology
├── go/
│   └── SKILL.md         # Go best practices
└── ...
```

### Skill Matching
```yaml
# Task tags → Skills
backend: [go, testing, debugging]
frontend: [typescript, react, testing]
bug: [debugging, git]
feature: [testing, git]
```

### Exit Criteria
- Skills auto-inject based on tags
- Can manually add/remove skills
- CLAUDE.md includes skill content

---

## Phase O4: Personas

**Goal:** Right mindset for the task

### Deliverables
- [ ] Persona definitions
- [ ] `ox pickup --as <persona>`
- [ ] `ox morph <persona>` - Switch mid-task
- [ ] `ox personas` - List personas
- [ ] Auto-select based on task tags

### Personas
```
~/.ox/personas/
├── builder.md           # Implementation focus
├── explorer.md          # Research focus
├── reviewer.md          # Quality focus
└── planner.md           # Architecture focus
```

### Persona Content
```markdown
# Builder Persona

You are in BUILDER mode. Focus on:
- Implementing working solutions
- Writing clean, tested code
- Shipping incrementally
- Pragmatic decisions over perfect

Avoid:
- Over-engineering
- Rabbit holes
- Scope creep
```

### Auto-Selection
```yaml
# Task tags → Persona
bug: builder
feature: builder
spike: explorer
research: explorer
rfc: planner
review: reviewer
```

### Exit Criteria
- Personas change AI behavior appropriately
- Auto-selection works
- Can override manually

---

## Phase O5: Memory & Checkpoints

**Goal:** Survive context resets

### Deliverables
- [ ] `ox checkpoint "description"` - Save state
- [ ] `ox resume` - Restore from checkpoint
- [ ] `ox checkpoints` - List checkpoints
- [ ] Auto-checkpoint before ox done
- [ ] Memory database for cross-session context

### Checkpoint Content
```yaml
task_id: "a3f8"
description: "Implemented auth middleware"
created_at: "2024-03-24T15:30:00Z"
files_changed:
  - "internal/auth/middleware.go"
  - "internal/auth/middleware_test.go"
context_summary: |
  Working on JWT validation.
  Middleware structure done.
  Next: add token refresh logic.
decisions:
  - "Using RS256 for JWT signing"
  - "Tokens expire in 1 hour"
blockers: []
```

### Exit Criteria
- Can checkpoint and resume
- Context survives session breaks
- Decisions are preserved

---

## Phase O6: Learning System

**Goal:** Get better over time

### Deliverables
- [ ] Capture outcome on task completion
- [ ] `ox learn "insight"` - Manual learning
- [ ] `ox learnings` - View learnings
- [ ] Surface relevant learnings on pickup
- [ ] Pattern detection over time

### Learning Store
```yaml
- task_type: "bug"
  tags: ["auth", "backend"]
  outcome: "success"
  duration: "2h"
  insight: "Auth bugs usually in token validation"

- task_type: "feature"
  tags: ["api"]
  outcome: "success"
  insight: "Always write tests first for APIs"
```

### Exit Criteria
- Learnings captured automatically
- Relevant learnings shown on new tasks
- Actually getting better

---

## Phase O7: Scheduling

**Goal:** Proactive assistance

### Deliverables
- [ ] `ox daily` - Morning briefing
- [ ] Deadline awareness from yoke
- [ ] `ox remind <task> <time>` - Set reminder
- [ ] Overdue task alerts
- [ ] Weekly summary

### Exit Criteria
- Never miss deadlines
- Daily focus is clear

---

## Phase O8: Multi-Agent (Future)

**Goal:** Supervisor delegates work

### Deliverables
- [ ] Supervisor agent
- [ ] Spawn sub-tasks
- [ ] Delegate to personas
- [ ] Progress tracking
- [ ] Parallel work

### Exit Criteria
- Complex tasks auto-decompose
- Multiple agents collaborate

---

## Future Ideas (Not Planned)

- TUI dashboard
- VS Code extension
- Workflow engine (multi-step automation)
- Team collaboration
- Custom agent providers

---

## Technical Decisions

### Why Go?
- Matches yoke (can import as library)
- Single binary
- Fast

### Workspace Strategy
- Git worktrees for isolation (preferred)
- Fallback: branch in same repo

### AI Integration
- Start with Claude Code CLI
- Abstract provider for future (Ollama, etc.)

---

## Status Key

- ✅ CURRENT - Active phase
- 🔜 NEXT - Coming up
- 📋 PLANNED - On roadmap
- 💡 IDEA - Not committed
- ⏳ WAITING - Blocked on dependency
