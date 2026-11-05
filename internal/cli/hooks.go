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
  ox-brief   Start every thread with ` + "`ox now --brief`" + ` (runs ` + "`ox hook session-start`" + `)

Custom hooks are shell scripts in ~/.ox/hooks/ that output JSON for Claude Code.`,
	RunE: runHooksList,
}

var hooksInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Register ox's hooks in Claude Code and check the start hook",
	Long: `Registers ox's SessionStart hooks in ~/.claude/settings.json, keeping every
hook that isn't ox's, then runs the start hook the way a thread would. If the
check fails, settings.json goes back as it was. ox setup does this too.`,
	SilenceUsage: true,
	RunE:         runHooksInit,
}

var hooksInstallCmd = &cobra.Command{
	Use:          "install",
	Short:        "Same as ox hooks init",
	SilenceUsage: true,
	RunE:         runHooksInit,
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

	fmt.Println("Available hooks:")
	fmt.Println()
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
	settings, err := hooks.SettingsPath()
	if err != nil {
		return err
	}
	if err := hooks.NewManager(cfg.Home).Setup(settings); err != nil {
		return err
	}
	fmt.Printf("Registered and checked in %s. New threads run it; restart open ones.\n", settings)
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
