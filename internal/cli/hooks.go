package cli

import (
	"fmt"

	"github.com/ashvinbhat/ox/internal/hooks"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage Claude Code hooks",
	Long: `Manages hooks that inject context into Claude Code sessions.

Built-in hooks:
  yoke-ready-tasks   Show ready tasks from yoke at session start
  ox-instructions    ox CLI quick reference
  workspace-context  Current workspace and task summary

Hooks are shell scripts in ~/.ox/hooks/ that output JSON for Claude Code.`,
	RunE: runHooksList,
}

var hooksInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize hooks and install to Claude Code",
	Long: `Creates built-in hook scripts and installs them to Claude Code settings.

This command:
1. Creates ~/.ox/hooks/ with built-in hook scripts
2. Updates ~/.claude/settings.json to register the hooks`,
	RunE: runHooksInit,
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install hooks to Claude Code settings",
	Long:  `Updates ~/.claude/settings.json to register ox hooks with Claude Code.`,
	RunE:  runHooksInstall,
}

var hooksRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a hook manually (for testing)",
	Args:  cobra.ExactArgs(1),
	RunE:  runHooksRun,
}

func runHooksList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	mgr := hooks.NewManager(cfg.Home)
	hookList := mgr.List()

	if len(hookList) == 0 {
		fmt.Println("No hooks found.")
		fmt.Println("Run 'ox hooks init' to create built-in hooks.")
		return nil
	}

	fmt.Println("Available hooks:\n")
	for _, h := range hookList {
		status := "enabled"
		if !h.Enabled {
			status = "disabled"
		}
		builtIn := ""
		if h.BuiltIn {
			builtIn = " (built-in)"
		}
		fmt.Printf("  %-20s [%s]%s\n", h.Name, status, builtIn)
		if h.Description != "" {
			fmt.Printf("                       %s\n", h.Description)
		}
	}
	fmt.Printf("\nHooks directory: %s\n", mgr.HooksDir())

	return nil
}

func runHooksInit(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	mgr := hooks.NewManager(cfg.Home)

	fmt.Println("Initializing hooks...")
	if err := mgr.Init(); err != nil {
		return fmt.Errorf("init hooks: %w", err)
	}

	fmt.Println("Created built-in hook scripts:")
	for _, h := range mgr.BuiltInHooks() {
		fmt.Printf("  - %s\n", h.Name)
	}

	fmt.Println("\nInstalling to Claude Code...")
	if err := mgr.InstallToClaudeCode(); err != nil {
		return fmt.Errorf("install to Claude Code: %w", err)
	}

	fmt.Println("Done! Hooks will run at next Claude Code session start.")
	fmt.Printf("\nHooks directory: %s\n", mgr.HooksDir())

	return nil
}

func runHooksInstall(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	mgr := hooks.NewManager(cfg.Home)

	fmt.Println("Installing hooks to Claude Code...")
	if err := mgr.InstallToClaudeCode(); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	fmt.Println("Done! Updated ~/.claude/settings.json")

	return nil
}

func runHooksRun(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	name := args[0]

	mgr := hooks.NewManager(cfg.Home)

	output, err := mgr.Run(name, "")
	if err != nil {
		return err
	}

	fmt.Println(output)
	return nil
}

func init() {
	hooksCmd.AddCommand(hooksInitCmd)
	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksRunCmd)
	rootCmd.AddCommand(hooksCmd)
}
