package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// yokeCmd passes through to yoke CLI for all task management
var yokeCmd = &cobra.Command{
	Use:   "yoke [command]",
	Short: "Pass-through to yoke CLI",
	Long: `Execute any yoke command directly through ox.

This gives you access to ALL yoke functionality:
  ox yoke tree           # Show task hierarchy
  ox yoke search <query> # Search tasks
  ox yoke edit <id>      # Edit a task
  ox yoke tag <id> <tag> # Add tag
  ox yoke block <id> <blocker> # Add dependency
  ox yoke subtask <parent> <title> # Create subtask
  ox yoke notes <id>     # Show notes
  ox yoke log <id>       # Show task history
  ox yoke import <url>   # Import from Notion
  ox yoke pull           # Pull from Notion
  ox yoke push           # Push to Notion

For full yoke help: ox yoke --help`,
	DisableFlagParsing: true,
	Run: func(cmd *cobra.Command, args []string) {
		yokePath := findYoke()
		if yokePath == "" {
			fmt.Fprintln(os.Stderr, "Error: yoke not found in PATH or ~/go/bin")
			os.Exit(1)
		}

		yokeCmd := exec.Command(yokePath, args...)
		yokeCmd.Stdin = os.Stdin
		yokeCmd.Stdout = os.Stdout
		yokeCmd.Stderr = os.Stderr

		if err := yokeCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			os.Exit(1)
		}
	},
}

// Convenience aliases for common yoke commands
var (
	treeCmd = &cobra.Command{
		Use:   "tree",
		Short: "Show task hierarchy (alias for: yoke tree)",
		Run:   runYokePassthrough("tree"),
	}

	searchCmd = &cobra.Command{
		Use:   "search <query>",
		Short: "Search tasks (alias for: yoke search)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("search"),
	}

	addCmd = &cobra.Command{
		Use:   "add <title>",
		Short: "Add a new task (alias for: yoke add)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("add"),
	}

	editCmd = &cobra.Command{
		Use:   "edit <task-id>",
		Short: "Edit a task (alias for: yoke edit)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("edit"),
	}

	subtaskCmd = &cobra.Command{
		Use:   "subtask <parent-id> <title>",
		Short: "Create a subtask (alias for: yoke subtask)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("subtask"),
	}

	blockCmd = &cobra.Command{
		Use:   "block <task-id> <blocker-id>",
		Short: "Add a blocker (alias for: yoke block)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("block"),
	}

	unblockCmd = &cobra.Command{
		Use:   "unblock <task-id> <blocker-id>",
		Short: "Remove a blocker (alias for: yoke unblock)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("unblock"),
	}

	tagCmd = &cobra.Command{
		Use:   "tag <task-id> <tag>",
		Short: "Add a tag (alias for: yoke tag)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("tag"),
	}

	untagCmd = &cobra.Command{
		Use:   "untag <task-id> <tag>",
		Short: "Remove a tag (alias for: yoke untag)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("untag"),
	}

	noteCmd = &cobra.Command{
		Use:   "note <task-id> <text>",
		Short: "Add a note (alias for: yoke note)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("note"),
	}

	notesCmd = &cobra.Command{
		Use:   "notes <task-id>",
		Short: "Show notes (alias for: yoke notes)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("notes"),
	}

	logCmd = &cobra.Command{
		Use:   "log <task-id>",
		Short: "Show task history (alias for: yoke log)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("log"),
	}

	tagsCmd = &cobra.Command{
		Use:   "tags",
		Short: "List all tags (alias for: yoke tags)",
		Run:   runYokePassthrough("tags"),
	}

	readyCmd = &cobra.Command{
		Use:   "ready",
		Short: "Show ready tasks (alias for: yoke ready)",
		DisableFlagParsing: true,
		Run:   runYokePassthrough("ready"),
	}
)

func runYokePassthrough(yokeCommand string) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		yokePath := findYoke()
		if yokePath == "" {
			fmt.Fprintln(os.Stderr, "Error: yoke not found")
			os.Exit(1)
		}

		fullArgs := append([]string{yokeCommand}, args...)
		yokeCmd := exec.Command(yokePath, fullArgs...)
		yokeCmd.Stdin = os.Stdin
		yokeCmd.Stdout = os.Stdout
		yokeCmd.Stderr = os.Stderr

		if err := yokeCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			os.Exit(1)
		}
	}
}

func findYoke() string {
	// Check PATH first
	if path, err := exec.LookPath("yoke"); err == nil {
		return path
	}

	// Check common locations
	home := os.Getenv("HOME")
	locations := []string{
		home + "/go/bin/yoke",
		"/usr/local/bin/yoke",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

func init() {
	rootCmd.AddCommand(yokeCmd)

	// Add convenience aliases
	rootCmd.AddCommand(treeCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(editCmd)
	rootCmd.AddCommand(subtaskCmd)
	rootCmd.AddCommand(blockCmd)
	rootCmd.AddCommand(unblockCmd)
	rootCmd.AddCommand(tagCmd)
	rootCmd.AddCommand(untagCmd)
	rootCmd.AddCommand(noteCmd)
	rootCmd.AddCommand(notesCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(tagsCmd)
	rootCmd.AddCommand(readyCmd)
}
