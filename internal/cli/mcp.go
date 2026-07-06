package cli

import (
	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/mcpserver"
)

var (
	mcpMission string
	mcpRole    string
	mcpAgent   string
)

var mcpCmd = &cobra.Command{
	Use:    "mcp",
	Short:  "Serve harness tools over MCP stdio (launched by claude sessions)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		return mcpserver.Run(cfg, mcpMission, mcpRole, mcpAgent)
	},
}

func init() {
	mcpCmd.Flags().StringVar(&mcpMission, "mission", "", "Mission ID (required)")
	mcpCmd.Flags().StringVar(&mcpRole, "role", "orchestrator", "orchestrator or worker")
	mcpCmd.Flags().StringVar(&mcpAgent, "agent", "", "Agent ID (worker role)")
	mcpCmd.MarkFlagRequired("mission")
	rootCmd.AddCommand(mcpCmd)
}
