package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/cmux"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

var cmuxCmd = &cobra.Command{
	Use:   "cmux",
	Short: "cmux viewport integration",
	Long: `Mirrors missions into the cmux terminal: a workspace per mission with the
orchestrator attached, a split per live worker, phase chips, and native
notifications on wake-ups. ox go and the watcher sync automatically; this
command is the manual lever.`,
}

var cmuxSyncCmd = &cobra.Command{
	Use:   "sync [mission-id]",
	Short: "Sync cmux workspaces with mission reality",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		if !cmux.Available() {
			return fmt.Errorf("cmux is not reachable — is the app running (and the socket password set)?")
		}
		if len(args) == 1 {
			m, err := mission.Open(cfg.Home, args[0])
			if err != nil {
				return err
			}
			cmux.SyncMission(cfg, m)
			fmt.Printf("synced %s\n", m.ID)
			return nil
		}
		missions, err := mission.List(cfg.Home)
		if err != nil {
			return err
		}
		for _, m := range missions {
			if !m.Open() || !tmuxutil.HasSession(m.TmuxSession()) {
				continue
			}
			cmux.SyncMission(cfg, m)
			fmt.Printf("synced %s\n", m.ID)
		}
		return nil
	},
}

func init() {
	cmuxCmd.AddCommand(cmuxSyncCmd)
	rootCmd.AddCommand(cmuxCmd)
}
