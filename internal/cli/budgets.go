package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/mission"
)

var budgetsCmd = &cobra.Command{
	Use:   "budgets [on|off]",
	Short: "Toggle spend-limit enforcement (tracking is always on)",
	Long: `Controls whether the harness enforces budgets: 70%/100% warnings, worker
wrap-up-then-kill at its limit, and the mission spend freeze. Cost TRACKING
(ledgers, spend in cockpit/missions) always stays on regardless.

  ox budgets        # show current state
  ox budgets on     # enforce limits
  ox budgets off    # track only (also unfreezes any frozen missions)`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()

		if len(args) == 0 {
			state := "OFF (tracking only)"
			if cfg.Budgets.Enforce {
				state = "ON"
			}
			fmt.Printf("Budget enforcement: %s\n", state)
			return nil
		}

		switch args[0] {
		case "on":
			cfg.Budgets.Enforce = true
		case "off":
			cfg.Budgets.Enforce = false
		default:
			return fmt.Errorf("use on or off")
		}
		if err := config.Save(cfg); err != nil {
			return err
		}

		if !cfg.Budgets.Enforce {
			missions, _ := mission.List(cfg.Home)
			for _, m := range missions {
				if m.Open() && m.SpendFrozen {
					mission.Update(cfg.Home, m.ID, func(mm *mission.Mission) error {
						mm.SpendFrozen = false
						return nil
					})
					fmt.Printf("Unfroze %s\n", m.ID)
				}
			}
		}
		fmt.Printf("Budget enforcement: %s\n", args[0])
		fmt.Println("Note: running watchers pick this up on their next mission read; restart a mission's watch window to apply immediately.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(budgetsCmd)
}
