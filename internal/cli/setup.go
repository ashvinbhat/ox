package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/claudeskills"
	"github.com/ashvinbhat/ox/internal/hooks"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install ox into Claude Code: the start hook, ox's skills and the board panel",
	Long: `Installs what every Claude Code thread picks up from ox:
  - the ox-brief SessionStart hook (same as ox hooks init)
  - ox's skills in ~/.claude/skills: /review-pr, /review-asks, /tidy, …
  - the ox board, a side panel of ox now, in ~/.claude/skills/ox-board

Safe to re-run after every upgrade. A folder you wrote yourself under one of
those names is left alone.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		mgr := hooks.NewManager(cfg.Home)
		if err := mgr.Init(); err != nil {
			return fmt.Errorf("init hooks: %w", err)
		}
		if err := mgr.InstallToClaudeCode(); err != nil {
			return fmt.Errorf("install hooks: %w", err)
		}
		fmt.Println("Hook: ox-brief (SessionStart)")

		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		installed, skipped, err := claudeskills.Install(filepath.Join(home, ".claude", "skills"))
		if err != nil {
			return fmt.Errorf("install skills: %w", err)
		}
		fmt.Printf("Skills and panel: %s\n", strings.Join(installed, ", "))
		for _, s := range skipped {
			fmt.Printf("Skipped %s: ~/.claude/skills/%s is yours, not ox's\n", s, s)
		}
		fmt.Println("New threads pick these up; restart open ones to use them.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
