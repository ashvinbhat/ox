package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/spf13/cobra"
)

var (
	repoName       string
	repoBaseBranch string
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage registered codebases",
	Long:  `Register, list, and remove codebases that ox can create workspaces for.`,
}

var repoAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Register and clone a codebase",
	Long: `Registers a codebase and clones it to ~/.ox/repos/.

Examples:
  ox repo add https://github.com/org/backend.git
  ox repo add git@github.com:org/frontend.git --name frontend
  ox repo add https://github.com/org/api.git --base-branch develop`,
	Args: cobra.ExactArgs(1),
	RunE: runRepoAdd,
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered codebases",
	RunE:  runRepoList,
}

var repoRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Unregister a codebase",
	Long: `Unregisters a codebase from ox.

By default, keeps the cloned files. Use --delete to remove them.`,
	Args: cobra.ExactArgs(1),
	RunE: runRepoRemove,
}

var repoRemoveDelete bool

func runRepoAdd(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	url := args[0]

	name := repoName
	if name == "" {
		name = repoNameFromURL(url)
	}
	if name == "" {
		return fmt.Errorf("cannot derive repo name from URL, use --name")
	}

	if _, exists := cfg.Repos[name]; exists {
		return fmt.Errorf("repo %q already registered", name)
	}

	dest := filepath.Join(cfg.Home, "repos", name)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("directory %s already exists", dest)
	}

	fmt.Printf("Cloning %s to %s...\n", url, dest)
	if err := gitutil.Clone(url, dest); err != nil {
		return fmt.Errorf("clone failed: %w", err)
	}

	baseBranch := repoBaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	cfg.Repos[name] = &config.RepoConfig{
		URL:        url,
		BaseBranch: baseBranch,
	}

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Registered repo %q\n", name)
	return nil
}

func runRepoList(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	if len(cfg.Repos) == 0 {
		fmt.Println("No repos registered")
		fmt.Println("\nRun 'ox repo add <url>' to register a codebase")
		return nil
	}

	fmt.Printf("%-15s %-12s %s\n", "NAME", "BRANCH", "URL")
	fmt.Println(strings.Repeat("-", 60))

	for name, rc := range cfg.Repos {
		branch := rc.BaseBranch
		if branch == "" {
			branch = "main"
		}
		fmt.Printf("%-15s %-12s %s\n", name, branch, rc.URL)
	}

	return nil
}

func runRepoRemove(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	name := args[0]

	if _, exists := cfg.Repos[name]; !exists {
		return fmt.Errorf("repo %q not registered", name)
	}

	if repoRemoveDelete {
		dest := filepath.Join(cfg.Home, "repos", name)
		fmt.Printf("Removing %s...\n", dest)
		if err := os.RemoveAll(dest); err != nil {
			return fmt.Errorf("remove failed: %w", err)
		}
	}

	delete(cfg.Repos, name)
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Unregistered repo %q\n", name)
	if !repoRemoveDelete {
		fmt.Println("(files kept, use --delete to remove)")
	}
	return nil
}

// repoNameFromURL extracts repo name from a git URL.
func repoNameFromURL(url string) string {
	// Handle various URL formats
	url = strings.TrimSuffix(url, ".git")

	// SSH: git@github.com:org/repo
	if strings.Contains(url, ":") && !strings.Contains(url, "://") {
		parts := strings.Split(url, ":")
		if len(parts) == 2 {
			path := parts[1]
			parts = strings.Split(path, "/")
			return parts[len(parts)-1]
		}
	}

	// HTTPS: https://github.com/org/repo
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
}

func init() {
	repoAddCmd.Flags().StringVarP(&repoName, "name", "n", "", "Name for the repo (default: derived from URL)")
	repoAddCmd.Flags().StringVarP(&repoBaseBranch, "base-branch", "b", "", "Base branch (default: main)")
	repoRemoveCmd.Flags().BoolVar(&repoRemoveDelete, "delete", false, "Also delete cloned files")

	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoRemoveCmd)
	rootCmd.AddCommand(repoCmd)
}
