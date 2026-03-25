package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	openRepoFlag string
)

var openCmd = &cobra.Command{
	Use:   "open [task-id]",
	Short: "Open workspace in IDE",
	Long: `Opens the task workspace in your configured IDE.

Opens the repo folder(s) directly, not the workspace root.
If multiple repos exist, opens all of them (or use --repo to specify one).

Uses the 'ide' setting from ox.yaml (default: windsurf).
Supported: windsurf, cursor, code/vscode, zed, idea, goland

Examples:
  ox open              # Open current workspace repos
  ox open 9            # Open workspace for task #9
  ox open --repo backend   # Open only the backend repo`,
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

	// Get repos to open
	var reposToOpen []string
	if openRepoFlag != "" {
		// Open specific repo
		found := false
		for _, r := range ws.Repos {
			if r == openRepoFlag {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repo %q not in workspace (available: %v)", openRepoFlag, ws.Repos)
		}
		reposToOpen = []string{openRepoFlag}
	} else if len(ws.Repos) > 0 {
		// Open all repos
		reposToOpen = ws.Repos
	} else {
		// No repos, open workspace root
		reposToOpen = []string{}
	}

	// Open repos
	if len(reposToOpen) == 0 {
		// Fallback to workspace root if no repos
		fmt.Printf("Opening %s in %s...\n", ws.Path, ide)
		execCmd := exec.Command(ideCmd, ws.Path)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		return execCmd.Start()
	}

	// Open each repo folder
	for _, repo := range reposToOpen {
		repoPath := filepath.Join(ws.Path, repo)

		// Resolve symlink to get actual worktree path
		resolved, err := filepath.EvalSymlinks(repoPath)
		if err != nil {
			fmt.Printf("Warning: could not resolve %s: %v\n", repo, err)
			resolved = repoPath
		}

		fmt.Printf("Opening %s (%s) in %s...\n", repo, resolved, ide)
		execCmd := exec.Command(ideCmd, resolved)
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		if err := execCmd.Start(); err != nil {
			return fmt.Errorf("failed to open %s: %w", repo, err)
		}
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
	openCmd.Flags().StringVar(&openRepoFlag, "repo", "", "Open specific repo only")
	rootCmd.AddCommand(openCmd)
}
