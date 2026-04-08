package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/personas"
	"github.com/spf13/cobra"
)

var (
	assistSkillsFlag  []string
	assistPersonaFlag string
)

var assistCmd = &cobra.Command{
	Use:   "assist",
	Short: "Start an AI assistant session with ox capabilities",
	Long: `Starts a Claude Code session with full ox knowledge and capabilities.

The assistant knows all ox commands, skills, personas, and workflows. It can:
- Execute any ox command (pickup, ship, done, review, etc.)
- Access and apply skills (backend, debug, testing, etc.)
- Adopt personas (builder, explorer, reviewer, captain)
- Help with general coding tasks
- Work with or without a yoke task

Examples:
  ox assist                        # General assistant with ox knowledge
  ox assist --persona builder      # Start with builder mindset
  ox assist --persona explorer     # Start in research/investigation mode
  ox assist --skill mongodb        # Load specific skill
  ox assist --skill debug,temporal # Load multiple skills`,
	RunE: runAssist,
}

func runAssist(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Create/update the ox assistant context file
	contextFile := filepath.Join(cfg.Home, "ASSISTANT.md")
	if err := generateAssistantContext(cfg.Home, contextFile); err != nil {
		return fmt.Errorf("generate context: %w", err)
	}

	// Build system prompt with ox knowledge
	systemPrompt := fmt.Sprintf("You are an ox-powered AI assistant. Read %s for your full capabilities. You can execute ox commands, use skills, and help with any coding task.", contextFile)

	// If persona specified, load it
	if assistPersonaFlag != "" {
		reg, err := personas.LoadRegistry(cfg.Home)
		if err == nil {
			if persona, ok := reg.Get(assistPersonaFlag); ok {
				personaPath := filepath.Join(cfg.Home, "personas", persona.Name+".md")
				systemPrompt += fmt.Sprintf(" Adopt the %s persona. Read %s for your mindset and approach.", persona.Name, personaPath)
			}
		}
	}

	// If skills specified, add them to system prompt
	if len(assistSkillsFlag) > 0 {
		skillPaths := []string{}
		for _, skill := range assistSkillsFlag {
			skillPath := filepath.Join(cfg.Home, "skills", skill+".md")
			if _, err := os.Stat(skillPath); err == nil {
				skillPaths = append(skillPaths, skillPath)
			}
		}
		if len(skillPaths) > 0 {
			systemPrompt += fmt.Sprintf(" Read these skills for expertise: %v", skillPaths)
		}
	}

	// Build claude command
	claudeArgs := []string{
		"--dangerously-skip-permissions",
		"--append-system-prompt", systemPrompt,
		"--add-dir", cfg.Home, // Give access to ox home
	}

	fmt.Println("🐂 Starting ox assistant...")
	if assistPersonaFlag != "" {
		fmt.Printf("   Persona: %s\n", assistPersonaFlag)
	}
	if len(assistSkillsFlag) > 0 {
		fmt.Printf("   Skills: %v\n", assistSkillsFlag)
	}
	fmt.Println("   Type 'exit' or Ctrl+C to quit")
	fmt.Println()

	// Launch claude
	claudeCmd := exec.Command("claude", claudeArgs...)
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr

	return claudeCmd.Run()
}

func generateAssistantContext(oxHome, outputPath string) error {
	content := `# Ox Assistant

You are an AI assistant powered by ox - an agent workspace manager for AI-assisted development.

## All Ox Commands

### Task Workflow
` + "```bash" + `
ox pickup <id> --repos <repo>    # Create workspace for yoke task
ox status                        # Show active workspaces
ox open [task-id]                # Open workspace in IDE
ox review [task-id]              # AI code review with Mycroft
ox review --local                # Review current directory (any git repo)
ox ship                          # Push branches and create PRs
ox done [id]                     # Complete task, cleanup workspace
` + "```" + `

### Personas (Mindsets)
` + "```bash" + `
ox personas                      # List available personas
ox morph <persona>               # Switch persona in current workspace
# Personas: builder, explorer, reviewer, captain
` + "```" + `

### Skills (Expertise)
` + "```bash" + `
ox skill list                    # List all skills
ox skill inject <name>           # Add skill to workspace
ox skill eject <name>            # Remove skill from workspace
` + "```" + `

### Progress & Memory
` + "```bash" + `
ox checkpoint --done "..." --next "..."  # Save progress
ox checkpoints                   # List checkpoints for task
ox resume                        # Show latest checkpoint context
ox learn "insight" -c category   # Capture a learning
ox learnings                     # List all learnings
` + "```" + `

### Task Management (yoke integration)
` + "```bash" + `
ox add "title"                   # Create task
ox subtask <parent> "title"      # Create subtask
ox tree                          # Show task hierarchy
ox ready                         # Show unblocked tasks
ox search "query"                # Search tasks
ox task <id>                     # Show task details
ox edit <id>                     # Edit task
ox tag <id> <tag>                # Add tag
ox untag <id> <tag>              # Remove tag
ox block <id> --by <blocker>     # Add dependency
ox unblock <id> <blocker>        # Remove dependency
ox note <id> "text"              # Add note
ox notes <id>                    # Show notes
ox log <id>                      # Show task history
` + "```" + `

### Repository Management
` + "```bash" + `
ox repo list                     # Show registered repos
ox repo add <url> --name <name>  # Register a codebase
ox repo remove <name>            # Unregister repo
ox worktree list                 # Show git worktrees
` + "```" + `

### Dashboard & Hooks
` + "```bash" + `
ox dashboard                     # Start web dashboard
ox hooks                         # List Claude Code hooks
ox hooks init                    # Install hooks to Claude Code
` + "```" + `

## Personas

| Persona | Mindset | Use When |
|---------|---------|----------|
| **builder** | Ship code, bias to action | Implementing features, fixing bugs |
| **explorer** | Research, investigate | Understanding code, spikes, learning |
| **reviewer** | Quality, correctness | Code review, audits, testing |
| **captain** | Plan, delegate, orchestrate | Epic planning, architecture, coordination |

Read persona files at: ` + oxHome + `/personas/

## Skills

Read skill files at: ` + oxHome + `/skills/

Key skills:
- **backend-engineer** - Java/Spring, Python, MongoDB patterns
- **debug-engineer** - Systematic debugging methodology
- **writing-tests** - Test writing (backend + API automation)
- **mongodb** - MongoDB queries and operations
- **temporal** - Temporal workflow debugging
- **aws-cli** - CloudWatch, AWS debugging
- **local-stack** - Local development environment
- **oodle** - Metrics and alerts

## Workflow Examples

### Starting a Task
` + "```bash" + `
ox ready                         # See what's unblocked
ox pickup 28 --repos backend     # Create workspace
cd ~/.ox/tasks/28-*/             # Enter workspace
# Claude reads AGENTS.md automatically
` + "```" + `

### During Work
` + "```bash" + `
ox checkpoint --done "Added API" --next "Write tests"
ox morph reviewer                # Switch to review mode
ox skill inject writing-tests    # Load test expertise
` + "```" + `

### Finishing
` + "```bash" + `
ox review                        # AI code review
ox ship                          # Push and create PR
ox done 28 --learn "Pattern X works well"
` + "```" + `

## Important Paths
- Ox home: ` + oxHome + `
- Skills: ` + oxHome + `/skills/
- Personas: ` + oxHome + `/personas/
- Tasks: ` + oxHome + `/tasks/
- Worktrees: ` + oxHome + `/worktrees/
- Config: ` + oxHome + `/ox.yaml

You are a capable general assistant AND an ox expert. Help with anything!
`

	return os.WriteFile(outputPath, []byte(content), 0644)
}

func init() {
	assistCmd.Flags().StringSliceVar(&assistSkillsFlag, "skill", nil, "Skills to load (comma-separated)")
	assistCmd.Flags().StringVar(&assistPersonaFlag, "persona", "", "Persona to adopt (builder, explorer, reviewer, captain)")
	rootCmd.AddCommand(assistCmd)
}
