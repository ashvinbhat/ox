package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/spf13/cobra"
)

var claudeCmd = &cobra.Command{
	Use:   "claude [task-id]",
	Short: "Start Claude in a workspace with full ox context",
	Long: `Starts a Claude Code session in a task workspace with full ox capabilities.

Unlike running 'claude' directly, this command:
- Loads full ox command reference
- Applies persona-specific guidance (captain orchestrates, builder implements)
- Injects relevant skills
- Enables checkpoint/learning reminders

Examples:
  ox claude              # Start in current workspace
  ox claude 18           # Start in workspace for task #18
  ox claude --persona captain  # Override persona`,
	Args: cobra.MaximumNArgs(1),
	RunE: runClaude,
}

var claudePersonaOverride string

func runClaude(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Find workspace
	var ws *workspace.TaskWorkspace
	var err error

	if len(args) > 0 {
		ws, err = workspace.Open(cfg.Home, args[0])
		if err != nil {
			return fmt.Errorf("workspace not found: %w", err)
		}
	} else {
		ws, err = getCurrentWorkspace(cfg.Home)
		if err != nil {
			return fmt.Errorf("not in a workspace - specify task ID or cd to workspace: %w", err)
		}
	}

	// Detect persona from workspace (or use override)
	persona := claudePersonaOverride
	if persona == "" {
		persona = detectPersonaFromWorkspace(ws.Path)
	}
	if persona == "" {
		persona = "captain" // default - orchestrate first, then delegate to builders
	}

	// Build persona-specific system prompt
	systemPrompt := buildPersonaPrompt(persona, cfg.Home, ws)

	// Build claude command
	claudeArgs := []string{
		"--dangerously-skip-permissions",
		"--append-system-prompt", systemPrompt,
		"--add-dir", cfg.Home,
	}

	fmt.Printf("🐂 Starting ox claude in workspace: %s\n", ws.Slug)
	fmt.Printf("   Persona: %s\n", persona)
	fmt.Printf("   Task: %s\n", filepath.Base(ws.Path))
	fmt.Println("   Type 'exit' or Ctrl+C to quit")
	fmt.Println()

	// Launch claude from workspace directory
	claudeCmd := exec.Command("claude", claudeArgs...)
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Dir = ws.Path

	return claudeCmd.Run()
}

func detectPersonaFromWorkspace(wsPath string) string {
	// Read AGENTS.md and look for persona line
	agentsPath := filepath.Join(wsPath, "AGENTS.md")
	content, err := os.ReadFile(agentsPath)
	if err != nil {
		return ""
	}

	// Simple parse - look for "# Persona: X"
	lines := string(content)
	for _, prefix := range []string{"# Persona: ", "Persona: "} {
		if idx := findLine(lines, prefix); idx != "" {
			return idx
		}
	}
	return ""
}

func findLine(content, prefix string) string {
	start := 0
	for {
		idx := indexOf(content[start:], prefix)
		if idx == -1 {
			return ""
		}
		idx += start
		// Find end of line
		end := idx + len(prefix)
		for end < len(content) && content[end] != '\n' {
			end++
		}
		return content[idx+len(prefix) : end]
	}
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func buildPersonaPrompt(persona, oxHome string, ws *workspace.TaskWorkspace) string {
	base := fmt.Sprintf(`You are an ox-powered AI assistant working in task workspace: %s

## Ox Commands Available
`, ws.Slug)

	// Common commands
	base += `
### Progress & Memory
- ox checkpoint --done "what you did" --next "what's next"  # IMPORTANT: Save progress regularly
- ox learn "insight" -c category  # Capture learnings
- ox resume  # See last checkpoint

### Task Management
- ox tree  # See task hierarchy
- ox subtask <parent> "title"  # Create subtask
- ox block <id> --by <blocker>  # Add dependency
- ox unblock <id> <blocker>  # Remove dependency
- ox done  # Complete current task

### Code Review & Ship
- ox review  # AI code review before shipping
- ox ship  # Push and create PR
`

	// Persona-specific guidance
	switch persona {
	case "captain":
		base += `
## Captain Persona - Orchestration Mode

You are in CAPTAIN mode. Your job is to ORCHESTRATE, not implement.

### Your Responsibilities
1. **Plan** - Break down the task into subtasks
2. **Organize** - Set up dependencies between tasks
3. **Delegate** - Create clear subtasks for builders
4. **Track** - Monitor progress, update blockers
5. **Coordinate** - Ensure pieces fit together

### Key Commands for Captains
- ox subtask <this-task> "Implementation: X"  # Create work items
- ox block <task> --by <other>  # Model dependencies
- ox tree  # Visualize the plan
- ox checkpoint --done "Planned X" --next "Coordinate Y"

### Captain Rules
- Do NOT write implementation code yourself
- DO create detailed subtasks with clear acceptance criteria
- DO think about order of operations and dependencies
- DO check on progress: ox tree, ox status
- DO document decisions in checkpoints

### Planning Template
When starting:
1. Read the task/epic description
2. Identify major components
3. Create subtasks for each component
4. Set up blocking relationships
5. Document the plan in a checkpoint
`

	case "explorer":
		base += `
## Explorer Persona - Research Mode

You are in EXPLORER mode. Your job is to INVESTIGATE and UNDERSTAND.

### Your Responsibilities
1. **Research** - Dig into code, docs, systems
2. **Document** - Record findings clearly
3. **Analyze** - Understand how things work
4. **Report** - Summarize for others

### Key Commands for Explorers
- ox learn "discovered X" -c finding  # Capture insights
- ox checkpoint --done "Investigated X" --next "Explore Y"
- ox note <task> "Finding: ..."  # Add notes to task

### Explorer Rules
- DO read extensively before concluding
- DO document your findings
- DO capture learnings for future reference
- AVOID making changes unless asked
- FOCUS on understanding, not implementing
`

	case "reviewer":
		base += `
## Reviewer Persona - Quality Mode

You are in REVIEWER mode. Your job is to ensure QUALITY.

### Your Responsibilities
1. **Review** - Check code changes thoroughly
2. **Test** - Verify functionality works
3. **Validate** - Ensure requirements are met
4. **Feedback** - Provide constructive feedback

### Key Commands for Reviewers
- ox review  # Run AI code review
- ox skill inject writing-tests  # Load test expertise
- ox checkpoint --done "Reviewed X" --next "Verify Y"

### Reviewer Rules
- DO run ox review before approving
- DO check for edge cases
- DO verify tests exist and pass
- DO look for security issues
- FOCUS on correctness and quality
`

	default: // builder
		base += `
## Builder Persona - Implementation Mode

You are in BUILDER mode. Your job is to SHIP working code.

### Your Responsibilities
1. **Understand** - Read existing code first
2. **Implement** - Write clean, minimal code
3. **Test** - Verify your changes work
4. **Ship** - Commit and push

### Key Commands for Builders
- ox review  # Review before shipping
- ox ship  # Push and create PR
- ox checkpoint --done "Implemented X" --next "Test Y"
- ox skill inject <name>  # Load expertise as needed

### Builder Rules
- DO follow existing patterns in the codebase
- DO write tests for new functionality
- DO keep changes focused and reviewable
- DO use ox review before shipping
- AVOID over-engineering - keep it simple
`
	}

	// Add skill paths
	base += fmt.Sprintf(`
## Skills Directory
Load skills with: ox skill inject <name>
Skills at: %s/skills/

## Workspace Info
- Path: %s
- Repos: %v
`, oxHome, ws.Path, ws.Repos)

	return base
}

func init() {
	claudeCmd.Flags().StringVar(&claudePersonaOverride, "persona", "", "Override workspace persona")
	rootCmd.AddCommand(claudeCmd)
}
