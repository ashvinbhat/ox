package cli

import (
	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/cockpit"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the live mission cockpit",
	Long: `Serves the ox cockpit on localhost: every open mission with phase, spend,
workers, and jobs — plus a live streaming terminal for any agent and an input
bar to talk to it directly.

Streams ride tmux control mode (real-time push, not polling); messages go
through the same hardened delivery path the harness itself uses.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		return cockpit.New(cfg, dashboardPort).Start()
	},
}

func init() {
	dashboardCmd.Flags().IntVarP(&dashboardPort, "port", "p", 8080, "Port to serve on")
	rootCmd.AddCommand(dashboardCmd)
}
