package cli

import (
	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/watcher"
)

var watchCmd = &cobra.Command{
	Use:   "watch <mission-id>",
	Short: "Run the mission watcher loop (normally lives in the mission's watch window)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		return watcher.New(cfg, args[0]).Run()
	},
}

func init() {
	rootCmd.AddCommand(watchCmd)
}
