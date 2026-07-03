package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/dashboard"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the Ox web dashboard",
	Long:  `Start a web-based dashboard for managing tasks, workspaces, and learnings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		yokePath := yokecli.BinaryPath()

		// Find ox binary (self)
		oxPath, err := os.Executable()
		if err != nil {
			oxPath = "ox"
		}

		server := dashboard.NewServer(dashboardPort, cfg.OxHome, yokePath, oxPath)
		return server.Start()
	},
}

func loadConfig() (*Config, error) {
	home := os.Getenv("HOME")
	oxHome := filepath.Join(home, ".ox")

	return &Config{
		OxHome: oxHome,
	}, nil
}

type Config struct {
	OxHome string
}

func init() {
	dashboardCmd.Flags().IntVarP(&dashboardPort, "port", "p", 8080, "Port to run the dashboard on")
	rootCmd.AddCommand(dashboardCmd)
}
