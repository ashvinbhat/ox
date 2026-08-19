package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/mission"
)

const trackGoalPrefix = "Track: "

var trackCmd = &cobra.Command{
	Use:   "track <name>",
	Short: "Open or resume a conductor for a track",
	Long: `A track is a theme or epic (e.g. "dm2", "email") with its own conductor:
a persistent session that holds the track's context, spawns missions for its
tasks, and relays their state back to you. It drives missions via the ox CLI
(ox go / inbox / retro / where) and never implements itself.

  ox track dm2        # open or resume the "dm2" conductor
  ox track            # list open track conductors

The conductor is a mission with the "conductor" playbook, so it resumes with
full context like any mission. Its plan.md is the track document.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		oxHome := requireConfig().Home

		if len(args) == 0 {
			return listTracks(oxHome)
		}
		name := strings.TrimSpace(args[0])
		if name == "" {
			return fmt.Errorf("track name required")
		}

		if m := findConductor(oxHome, name); m != nil {
			fmt.Printf("Resuming conductor %s for track %q\n", m.ID, name)
			return launchMission(oxHome, m, true)
		}
		fmt.Printf("Starting a new conductor for track %q\n", name)
		return createMission(oxHome, "conductor", trackGoalPrefix+name, nil, "")
	},
}

// findConductor returns the open conductor mission for a track, if any.
func findConductor(oxHome, name string) *mission.Mission {
	missions, err := mission.List(oxHome)
	if err != nil {
		return nil
	}
	want := trackGoalPrefix + name
	for _, m := range missions {
		if m.Open() && m.Type == "conductor" && m.Goal == want {
			return m
		}
	}
	return nil
}

func listTracks(oxHome string) error {
	missions, err := mission.List(oxHome)
	if err != nil {
		return err
	}
	found := false
	for _, m := range missions {
		if m.Open() && m.Type == "conductor" {
			if !found {
				fmt.Println("Open track conductors:")
				found = true
			}
			fmt.Printf("  %-6s %s\n", m.ID, strings.TrimPrefix(m.Goal, trackGoalPrefix))
		}
	}
	if !found {
		fmt.Println("No open track conductors. Start one with `ox track <name>`.")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(trackCmd)
}
