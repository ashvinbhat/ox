package cli

import (
	"fmt"
	"os"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/hooks"
	"github.com/ashvinbhat/ox/internal/personas"
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
  - personas/  (persona definitions)
  - hooks/     (Claude Code hooks)`,
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

	// Create default hooks
	if err := createDefaultHooks(oxHome); err != nil {
		fmt.Printf("Warning: failed to create default hooks: %v\n", err)
	}

	fmt.Printf("Initialized ox at %s\n", oxHome)
	fmt.Println("\nNext steps:")
	fmt.Println("  ox repo add <url>              # Register a codebase")
	fmt.Println("  ox pickup <task-id> --repos x  # Create workspace for yoke task")

	return nil
}

func createDefaultPersonas(oxHome string) error {
	return personas.CreateDefaultPersonas(oxHome)
}

func createDefaultHooks(oxHome string) error {
	mgr := hooks.NewManager(oxHome)
	return mgr.Init()
}

func init() {
	rootCmd.AddCommand(initCmd)
}
