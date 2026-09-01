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
	Short: "A mission harness for AI-assisted development",
	Long: `Ox is a mission harness built on yoke.

A mission opens a persistent orchestrator that plans, delegates to workers and
jobs, integrates, and ships — resumable at any time.

Quick start:
  ox init              # Initialize ~/.ox
  ox repo add <url>    # Register a codebase
  ox go <task-ref>     # Start or resume a mission
  ox                   # Resume the current mission`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip config loading for root init command only
		if cmd.Name() == "init" && cmd.Parent() != nil && cmd.Parent().Name() == "ox" {
			return
		}
		cfg, cfgErr = config.Load()
	},
	// Bare `ox` is the fast path into work: resume (and attach to) the current
	// mission's orchestrator — same as `ox go` with no args. A stray positional
	// that matched no subcommand falls here too; show help rather than guess.
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return cmd.Help()
		}
		return runGo(cmd, nil)
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
