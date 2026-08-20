package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
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

		// Membership management (does not launch the conductor).
		if len(trackAttach) > 0 || len(trackDetach) > 0 {
			return setTrackMembership(oxHome, name, trackAttach, trackDetach)
		}
		if trackListMissions {
			return printTrackMissions(oxHome, name)
		}
		if trackBoard {
			return runTrackBoard(oxHome, name)
		}

		if m := findConductor(oxHome, name); m != nil {
			fmt.Printf("Resuming conductor %s for track %q\n", m.ID, name)
			printTrackMissions(oxHome, name)
			return launchMission(oxHome, m, true)
		}
		fmt.Printf("Starting a new conductor for track %q\n", name)
		return createMission(oxHome, "conductor", trackGoalPrefix+name, nil, "")
	},
}

var (
	trackAttach       []string
	trackDetach       []string
	trackListMissions bool
	trackBoard        bool
)

// runTrackBoard renders a live, auto-refreshing board of the track's missions
// and what needs the user — the conductor's status pane.
func runTrackBoard(oxHome, name string) error {
	for {
		fmt.Print("\033[2J\033[H") // clear + home
		fmt.Printf("  ╭─ TRACK: %s ─ %s\n\n", name, time.Now().Format("15:04:05"))
		missions, err := mission.List(oxHome)
		if err != nil {
			fmt.Printf("  (error: %v)\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		var mine []*mission.Mission
		for _, m := range missions {
			if m.Track == name {
				mine = append(mine, m)
			}
		}
		if len(mine) == 0 {
			fmt.Println("  no missions attached yet.")
			fmt.Printf("\n  attach: ox track %s --attach <id>,<id>\n", name)
		} else {
			fmt.Println("  MISSIONS")
			for _, m := range mine {
				state := m.Phase
				if !m.Open() {
					state = "closed"
				}
				fmt.Printf("   %-5s %-10s %s\n", m.ID, state, firstN(m.Goal, 30))
				if pend := harness.PendingOnUser(m); pend != "" {
					fmt.Printf("         ⚠ %s\n", firstN(pend, 34))
				}
			}
		}
		fmt.Print("\n  ─────────────\n  refreshes 5s · Ctrl-a z zoom\n")
		time.Sleep(5 * time.Second)
	}
}

// setTrackMembership sets/clears the Track field on the given missions.
func setTrackMembership(oxHome, name string, attach, detach []string) error {
	set := func(id, track string) error {
		if _, err := mission.Update(oxHome, id, func(m *mission.Mission) error {
			m.Track = track
			return nil
		}); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		return nil
	}
	for _, id := range attach {
		if err := set(id, name); err != nil {
			return err
		}
		fmt.Printf("attached %s → track %q\n", id, name)
	}
	for _, id := range detach {
		if err := set(id, ""); err != nil {
			return err
		}
		fmt.Printf("detached %s from track %q\n", id, name)
	}
	return nil
}

// printTrackMissions lists the missions attached to a track, newest first.
func printTrackMissions(oxHome, name string) error {
	missions, err := mission.List(oxHome)
	if err != nil {
		return err
	}
	var any bool
	for _, m := range missions {
		if m.Track != name {
			continue
		}
		if !any {
			fmt.Printf("  track %q missions:\n", name)
			any = true
		}
		state := m.Phase
		if !m.Open() {
			state = "closed"
		}
		fmt.Printf("    %-6s %-10s %s\n", m.ID, state, firstN(m.Goal, 48))
	}
	if !any {
		fmt.Printf("  track %q has no attached missions yet (attach with `ox track %s --attach <mission-id>...`)\n", name, name)
	}
	return nil
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
	trackCmd.Flags().StringSliceVar(&trackAttach, "attach", nil, "Attach existing missions to the track (mission ids)")
	trackCmd.Flags().StringSliceVar(&trackDetach, "detach", nil, "Detach missions from the track (mission ids)")
	trackCmd.Flags().BoolVar(&trackListMissions, "missions", false, "List the track's attached missions and exit")
	trackCmd.Flags().BoolVar(&trackBoard, "board", false, "Render a live board of the track's missions (used by the conductor pane)")
	rootCmd.AddCommand(trackCmd)
}
