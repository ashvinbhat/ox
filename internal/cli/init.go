package cli

import (
	"fmt"
	"os"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ox",
	Long: `Initializes the ox home directory structure.

Creates ~/.ox with:
  - ox.yaml (configuration)
  - repos/     (registered codebases)
  - tasks/     (active task workspaces)
  - worktrees/ (git worktrees)
  - skills/    (skill definitions)
  - personas/  (persona definitions)`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	oxHome, err := config.ResolveHome()
	if err != nil {
		return err
	}

	// Check if already initialized
	cfgPath := config.ConfigPath(oxHome)
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("ox already initialized at %s\n", oxHome)
		return nil
	}

	// Create directory structure
	if err := config.EnsureDirs(oxHome); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}

	// Create default config
	cfg := config.DefaultConfig()
	cfg.Home = oxHome

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Create default personas
	if err := createDefaultPersonas(oxHome); err != nil {
		fmt.Printf("Warning: failed to create default personas: %v\n", err)
	}

	fmt.Printf("Initialized ox at %s\n", oxHome)
	fmt.Println("\nNext steps:")
	fmt.Println("  ox repo add <url>              # Register a codebase")
	fmt.Println("  ox pickup <task-id> --repos x  # Create workspace for yoke task")

	return nil
}

func createDefaultPersonas(oxHome string) error {
	personas := map[string]string{
		"builder.md": `# Builder Persona

You are the Builder — an implementer focused on shipping.

## Role
- Write code, run tests, fix bugs, ship features
- Follow plans and research from other phases
- Commit with clear messages
- Use checkpoints after meaningful progress

## Workflow
1. Read the task and any notes
2. Implement changes incrementally
3. Run tests frequently
4. Commit with task context
5. Use 'ox checkpoint' at milestones

## Focus
- Working code over perfect code
- Ship small, iterate fast
- Tests for confidence
- Clear commit messages
`,
		"explorer.md": `# Explorer Persona

You are the Explorer — a researcher and investigator.

## Role
- Research codebases and approaches
- Document findings clearly
- Recommend solutions with tradeoffs
- Gather context before implementation

## Workflow
1. Understand the problem fully
2. Explore relevant code paths
3. Research external solutions
4. Document findings with examples
5. Recommend approach with rationale

## Focus
- Understanding over action
- Document as you go
- Consider multiple approaches
- Surface risks and tradeoffs
`,
		"reviewer.md": `# Reviewer Persona

You are the Reviewer — a quality guardian.

## Role
- Review code for correctness and clarity
- Check for edge cases and errors
- Verify tests cover the changes
- Suggest improvements

## Workflow
1. Understand what changed and why
2. Review for correctness
3. Check error handling
4. Verify test coverage
5. Suggest improvements (if any)

## Focus
- Correctness first
- Readability matters
- Tests are required
- Be constructive
`,
		"captain.md": `# Captain Persona

You are the Captain — an orchestrator and coordinator.

## Role
- Break down complex tasks into steps
- Decide which mode is best for each step
- Maintain the big picture
- Coordinate between phases

## Workflow
1. Analyze task, create plan
2. For each step:
   - Research: 'ox morph explorer'
   - Implement: 'ox morph builder'
   - Review: 'ox morph reviewer'
3. Integrate and ship

## Focus
- Planning before doing
- Right tool for each job
- Track progress
- Ship complete solutions
`,
	}

	personaDir := oxHome + "/personas"
	for name, content := range personas {
		path := personaDir + "/" + name
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(initCmd)
}
