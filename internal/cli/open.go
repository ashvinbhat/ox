package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open [task-id]",
	Short: "Open workspace in IDE",
	Long: `Opens the task workspace in your configured IDE.

Uses the 'ide' setting from ox.yaml (default: windsurf).
Supported: windsurf, cursor, code/vscode, zed, idea, goland

Examples:
  ox open          # Open current workspace
  ox open 9        # Open workspace for task #9`,
	Args: cobra.MaximumNArgs(1),
	RunE: runOpen,
}

func runOpen(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	// Find workspace
	var ws *workspace.TaskWorkspace
	var err error

	if len(args) > 0 {
		ws, err = workspace.Open(cfg.Home, args[0])
	} else {
		workspaces, listErr := workspace.List(cfg.Home)
		if listErr != nil {
			return fmt.Errorf("list workspaces: %w", listErr)
		}
		if len(workspaces) == 0 {
			return fmt.Errorf("no active workspaces")
		}
		if len(workspaces) > 1 {
			return fmt.Errorf("multiple workspaces active, specify task ID")
		}
		ws = workspaces[0]
	}

	if err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	// Determine IDE command
	ide := cfg.IDE
	if ide == "" {
		ide = "windsurf"
	}

	ideCmd := getIDECommand(ide)
	if ideCmd == "" {
		return fmt.Errorf("unknown IDE: %s (supported: cursor, code, vscode, zed)", ide)
	}

	fmt.Printf("Opening %s in %s...\n", ws.Path, ide)

	// Open the workspace
	execCmd := exec.Command(ideCmd, ws.Path)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Start(); err != nil {
		return fmt.Errorf("failed to open IDE: %w", err)
	}

	return nil
}

// getIDECommand returns the command to open a directory in the IDE.
func getIDECommand(ide string) string {
	switch ide {
	case "windsurf":
		return "windsurf"
	case "cursor":
		return "cursor"
	case "code", "vscode":
		return "code"
	case "zed":
		return "zed"
	case "idea":
		return "idea"
	case "goland":
		return "goland"
	default:
		return ""
	}
}

func init() {
	rootCmd.AddCommand(openCmd)
}
