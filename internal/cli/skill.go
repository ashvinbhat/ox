package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ashvinbhat/ox/internal/skills"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skills",
	Long:  `List, add, remove, and inject skills into workspaces.`,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available skills",
	Long: `Lists all registered skills with their metadata.

Shows which tags, personas, and task types trigger each skill.`,
	RunE: runSkillList,
}

func runSkillList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	reg, err := skills.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	if len(reg.Skills) == 0 {
		fmt.Println("No skills registered")
		fmt.Println("\nAdd skills with: ox skill add <name> --file <path>")
		return nil
	}

	// Sort by name
	names := reg.List()
	sort.Strings(names)

	fmt.Printf("%-15s %-20s %-20s %-15s %s\n", "NAME", "TAGS", "PERSONAS", "TASK TYPES", "FILE")
	fmt.Println(strings.Repeat("-", 85))

	for _, name := range names {
		skill := reg.Skills[name]
		tags := strings.Join(skill.Tags, ", ")
		if tags == "" {
			tags = "-"
		}
		personas := strings.Join(skill.Personas, ", ")
		if personas == "" {
			personas = "-"
		}
		taskTypes := strings.Join(skill.TaskTypes, ", ")
		if taskTypes == "" {
			taskTypes = "-"
		}

		fmt.Printf("%-15s %-20s %-20s %-15s %s\n", name, truncate(tags, 20), truncate(personas, 20), truncate(taskTypes, 15), skill.File)
	}

	return nil
}

var (
	skillAddFile        string
	skillAddTags        []string
	skillAddPersonas    []string
	skillAddTaskTypes   []string
	skillAddDescription string
)

var skillAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Register a new skill",
	Long: `Registers a new skill with metadata for auto-injection.

The skill file should be a markdown file in ~/.ox/skills/.

Examples:
  ox skill add java --file java.md --tags backend,java --personas builder
  ox skill add debugging --tags bug,incident --personas builder,explorer
  ox skill add testing --personas builder,reviewer --task-types test`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillAdd,
}

func runSkillAdd(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	name := args[0]

	reg, err := skills.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	// Default file name
	file := skillAddFile
	if file == "" {
		file = name + ".md"
	}

	skill := &skills.Skill{
		File:        file,
		Description: skillAddDescription,
		Tags:        skillAddTags,
		Personas:    skillAddPersonas,
		TaskTypes:   skillAddTaskTypes,
	}

	if err := reg.Add(name, skill); err != nil {
		return err
	}

	fmt.Printf("Added skill: %s\n", name)
	fmt.Printf("  File:       %s\n", file)
	if len(skill.Tags) > 0 {
		fmt.Printf("  Tags:       %s\n", strings.Join(skill.Tags, ", "))
	}
	if len(skill.Personas) > 0 {
		fmt.Printf("  Personas:   %s\n", strings.Join(skill.Personas, ", "))
	}
	if len(skill.TaskTypes) > 0 {
		fmt.Printf("  Task types: %s\n", strings.Join(skill.TaskTypes, ", "))
	}

	// Check if file exists
	skillPath := filepath.Join(cfg.Home, "skills", file)
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		fmt.Printf("\nNote: Create the skill file at: %s\n", skillPath)
	}

	return nil
}

var skillRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Unregister a skill",
	Long: `Removes a skill from the registry.

Does not delete the skill file, only removes it from auto-injection.

Example:
  ox skill remove java`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillRemove,
}

func runSkillRemove(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	name := args[0]

	reg, err := skills.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	if err := reg.Remove(name); err != nil {
		return err
	}

	fmt.Printf("Removed skill: %s\n", name)
	return nil
}

var skillInjectCmd = &cobra.Command{
	Use:   "inject <name>",
	Short: "Add skill to current workspace",
	Long: `Injects a skill into the current task workspace.

Creates a symlink to the skill file in the workspace.
The skill will be included in AGENTS.md on next regeneration.

Example:
  ox skill inject debugging`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillInject,
}

func runSkillInject(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	name := args[0]

	reg, err := skills.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	skill, ok := reg.Get(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	// Find current workspace
	workspaces, err := workspace.List(cfg.Home)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("no active workspace")
	}
	if len(workspaces) > 1 {
		return fmt.Errorf("multiple workspaces active")
	}

	ws := workspaces[0]

	// Create .skills directory in workspace if needed
	skillsDir := filepath.Join(ws.Path, ".skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("create .skills dir: %w", err)
	}

	// Symlink skill file
	src := reg.SkillPath(skill)
	dst := filepath.Join(skillsDir, skill.File)

	// Remove if exists
	os.Remove(dst)

	if err := os.Symlink(src, dst); err != nil {
		return fmt.Errorf("create symlink: %w", err)
	}

	fmt.Printf("Injected skill: %s\n", name)
	fmt.Printf("  Linked to: %s\n", dst)
	fmt.Println("\nRegenerate AGENTS.md with: ox refresh")

	return nil
}

var skillEjectCmd = &cobra.Command{
	Use:   "eject <name>",
	Short: "Remove skill from current workspace",
	Long: `Removes a skill from the current task workspace.

Removes the symlink to the skill file.

Example:
  ox skill eject debugging`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillEject,
}

func runSkillEject(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	name := args[0]

	reg, err := skills.LoadRegistry(cfg.Home)
	if err != nil {
		return fmt.Errorf("load skills: %w", err)
	}

	skill, ok := reg.Get(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	// Find current workspace
	workspaces, err := workspace.List(cfg.Home)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("no active workspace")
	}
	if len(workspaces) > 1 {
		return fmt.Errorf("multiple workspaces active")
	}

	ws := workspaces[0]

	// Remove symlink
	dst := filepath.Join(ws.Path, ".skills", skill.File)
	if err := os.Remove(dst); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill %q not injected in workspace", name)
		}
		return fmt.Errorf("remove symlink: %w", err)
	}

	fmt.Printf("Ejected skill: %s\n", name)
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func init() {
	skillAddCmd.Flags().StringVar(&skillAddFile, "file", "", "Skill file name (default: <name>.md)")
	skillAddCmd.Flags().StringSliceVar(&skillAddTags, "tags", nil, "Tags that trigger this skill")
	skillAddCmd.Flags().StringSliceVar(&skillAddPersonas, "personas", nil, "Personas that use this skill")
	skillAddCmd.Flags().StringSliceVar(&skillAddTaskTypes, "task-types", nil, "Task types that trigger this skill")
	skillAddCmd.Flags().StringVar(&skillAddDescription, "desc", "", "Short description")

	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillAddCmd)
	skillCmd.AddCommand(skillRemoveCmd)
	skillCmd.AddCommand(skillInjectCmd)
	skillCmd.AddCommand(skillEjectCmd)

	rootCmd.AddCommand(skillCmd)
}
