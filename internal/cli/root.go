package cli

import (
	"fmt"
	"os"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfg     *config.Config
	cfgErr  error
	verbose bool
)

// SetVersion wires the build-stamped version into the root command so
// `ox --version` / `ox version` report it.
func SetVersion(v string) {
	rootCmd.Version = v
	rootCmd.InitDefaultVersionFlag()
}

var rootCmd = &cobra.Command{
	Use:   "ox",
	Short: "What your Claude Code threads draw on",
	Long: `ox is what Claude Code desktop threads draw on: where every thread stands,
your tracks, yoke tasks, review history and long-term memory.

Quick start:
  ox init              # Initialize ~/.ox
  ox hooks init        # Start every thread with ox now --brief
  ox                   # Same as ox now: threads by state and track

Missions (ox go) are frozen: they still run, but get no new work.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip config loading for root init command only
		if cmd.Name() == "init" && cmd.Parent() != nil && cmd.Parent().Name() == "ox" {
			return
		}
		cfg, cfgErr = config.Load()
	},
	// Bare `ox` answers "where do things stand?" — same as `ox now`. A stray
	// positional that matched no subcommand falls here too; show help rather
	// than guess.
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return cmd.Help()
		}
		return runNow(cmd, nil)
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
}

// requireConfig ensures config is loaded, exits with error if not.
func requireConfig() *config.Config {
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", cfgErr)
		fmt.Fprintf(os.Stderr, "Run 'ox init' to initialize ox.\n")
		os.Exit(1)
	}
	return cfg
}
