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
	Short: "Install ox into Claude Code: the start hook, ox's skills and the board panel",
	Long: `Installs what every Claude Code thread picks up from ox:
  - the ox-brief SessionStart hook, ` + "`ox hook session-start`" + `, and the ask hook,
    ` + "`ox hook ask`" + `, then runs each once the way a thread would; if either
    fails, settings.json goes back as it was
  - the stop list, ~/.ox/stops.md: what a thread stops and asks you for
    (written once; your edits stay)
  - ox's skills in ~/.claude/skills: /review-pr, /review-asks, /tidy, …
  - the ox board, a side panel of ox now, in ~/.claude/skills/ox-board
  - board reload: ` + hooks.WatchEnv + `=1 in settings.json's env, so a
    desktop thread reloads the board when it changes (a value you set stays)

Safe to re-run after every upgrade. A folder you wrote yourself under one of
those names is left alone.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		settings, err := hooks.SettingsPath()
		if err != nil {
			return err
		}
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
		switch v := hooks.Watching(settings); v {
		case "1":
			fmt.Println("Board reload: on; threads started from now pick up a new board without a restart")
		case "":
			fmt.Printf("Board reload: off; settings.json's env isn't an object, so %s couldn't be added\n", hooks.WatchEnv)
		default:
			fmt.Printf("Board reload: off; settings.json sets %s to %q, which ox keeps\n", hooks.WatchEnv, v)
		}

		home, err := livepath.Home()
		if err != nil {
			return err
		}
		installed, skipped, removed, err := claudeskills.Install(filepath.Join(home, ".claude", "skills"))
		if err != nil {
			return fmt.Errorf("install skills: %w", err)
		}
		fmt.Printf("Skills and panel: %s\n", strings.Join(installed, ", "))
		for _, s := range skipped {
			fmt.Printf("Skipped %s: ~/.claude/skills/%s is yours, not ox's\n", s, s)
		}
		for _, s := range removed {
			fmt.Printf("Removed %s: ox no longer ships it\n", s)
		}
		fmt.Println("New threads pick these up; restart open ones for new skills.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
