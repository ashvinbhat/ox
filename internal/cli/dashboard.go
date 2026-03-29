package cli

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/dashboard"
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

		// Find yoke binary
		yokePath := findYoke()
		if yokePath == "" {
			yokePath = "yoke" // fallback to PATH
		}

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

// findYoke is defined in yoke.go but we use it here too
func findYokeForDashboard() string {
	// Check PATH first
	if path, err := exec.LookPath("yoke"); err == nil {
		return path
	}

	// Check common locations
	home := os.Getenv("HOME")
	locations := []string{
		home + "/go/bin/yoke",
		"/usr/local/bin/yoke",
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

func init() {
	dashboardCmd.Flags().IntVarP(&dashboardPort, "port", "p", 8080, "Port to run the dashboard on")
	rootCmd.AddCommand(dashboardCmd)
}
