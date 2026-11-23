package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/claudeskills"
	"github.com/ashvinbhat/ox/internal/hooks"
	"github.com/ashvinbhat/ox/internal/livepath"
	"github.com/ashvinbhat/ox/internal/stops"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install ox into Claude Code: the start and ask hooks and ox's skills",
	Long: `Installs what every Claude Code thread picks up from ox:
  - the ox-brief SessionStart hook, ` + "`ox hook session-start`" + `, and the ask hook,
    ` + "`ox hook ask`" + `, then runs each once the way a thread would; if either
    fails, settings.json goes back as it was
  - the stop list, ~/.ox/stops.md: what a thread stops and asks you for
    (written once; your edits stay)
  - ox's skills in ~/.claude/skills: /review-pr, /review-asks, /tidy, …

It retires what earlier versions installed: the ox board panel
(~/.claude/skills/ox-board) and its reload switch, ` + hooks.WatchEnv + `=1
in settings.json's env.

Safe to re-run after every upgrade. A folder you wrote yourself under one of
those names, or a reload value you set, is left alone.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		settings, err := hooks.SettingsPath()
		if err != nil {
			return err
		}
		watched := hooks.Watching(settings)
		if err := hooks.NewManager(cfg.Home).Setup(settings); err != nil {
			return err
		}
		fmt.Println("Hook: ox-brief (SessionStart), checked: it prints the brief")
		fmt.Println("Hook: ox ask (before a question to you), checked: it sends a plain question back")
		wrote, err := stops.Install(cfg.Home)
		if err != nil {
			return fmt.Errorf("write the stop list: %w", err)
		}
		if wrote {
			fmt.Printf("Stops: %s written with the default list\n", stops.Path(cfg.Home))
		} else {
			fmt.Printf("Stops: %s is yours, kept\n", stops.Path(cfg.Home))
		}
		if watched == "1" && hooks.Watching(settings) == "" {
			fmt.Printf("Removed %s=1 from settings.json's env: ox set it for the board\n", hooks.WatchEnv)
		}

		home, err := livepath.Home()
		if err != nil {
			return err
		}
		installed, skipped, removed, err := claudeskills.Install(filepath.Join(home, ".claude", "skills"))
		if err != nil {
			return fmt.Errorf("install skills: %w", err)
		}
		fmt.Printf("Skills: %s\n", strings.Join(installed, ", "))
		for _, s := range skipped {
			fmt.Printf("Skipped %s: ~/.claude/skills/%s is yours, not ox's\n", s, s)
		}
		for _, s := range removed {
			fmt.Printf("Removed ~/.claude/skills/%s: ox no longer ships it\n", s)
		}
		fmt.Println("New threads pick these up; restart open ones for new skills.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
