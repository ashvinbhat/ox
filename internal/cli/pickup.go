package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/context"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/learning"
	"github.com/ashvinbhat/ox/internal/personas"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokehelper"
	"github.com/spf13/cobra"
)

var (
	pickupRepos   []string
	pickupPersona string
)

var pickupCmd = &cobra.Command{
	Use:   "pickup <task-id>",
	Short: "Create workspace for a yoke task",
	Long: `Creates a task workspace with git worktrees and context.

This command:
1. Fetches task details from yoke
2. Creates a workspace directory in ~/.ox/tasks/
3. Creates git worktrees for specified repos
4. Generates CLAUDE.md with task context and persona

Examples:
  ox pickup 9 --repos backend
  ox pickup 9 --repos backend,frontend --persona builder`,
	Args: cobra.ExactArgs(1),
	RunE: runPickup,
}

func runPickup(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	taskRef := args[0]

	// Check if repos specified
	if len(pickupRepos) == 0 {
		return fmt.Errorf("at least one repo required (use --repos)")
	}

	// Validate repos exist
	for _, r := range pickupRepos {
		if _, exists := cfg.Repos[r]; !exists {
			return fmt.Errorf("repo %q not registered (run 'ox repo list' to see available)", r)
		}
	}

	// Load yoke task
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return fmt.Errorf("open yoke: %w", err)
	}
	defer yokeClient.Close()

	t, err := yokeClient.Get(taskRef)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// Check if workspace already exists
	if existing, err := workspace.Open(cfg.Home, taskRef); err == nil {
		fmt.Printf("Workspace already exists: %s\n", existing.Path)
		fmt.Println("Use 'ox work' to resume or 'ox done' to close it first.")
		return nil
	}

	// Create workspace
	fmt.Printf("Creating workspace for task #%d: %s\n", t.Seq, t.Title)
	ws, err := workspace.Create(cfg.Home, t.ID, t.Seq, t.Title)
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	// Create worktrees and symlinks for each repo
	for _, repoName := range pickupRepos {
		rc := cfg.Repos[repoName]
		repoPath := filepath.Join(cfg.Home, "repos", repoName)

		// Fetch latest - must succeed to ensure worktree starts from fresh code
		fmt.Printf("Fetching %s...\n", repoName)
		if err := gitutil.Fetch(repoPath); err != nil {
			return fmt.Errorf("fetch %s failed (need latest origin/main): %w", repoName, err)
		}

		// Create worktree
		branchName := fmt.Sprintf("ox/%d-%s", t.Seq, slugify(t.Title))
		worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", t.Seq))

		// Ensure worktree parent dir exists
		os.MkdirAll(filepath.Dir(worktreePath), 0o755)

		baseBranch := rc.BaseBranch
		if baseBranch == "" {
			baseBranch = "origin/main"
		}
		// Ensure we use remote ref for fresh code (if not already a remote ref)
		if !strings.HasPrefix(baseBranch, "origin/") && !strings.Contains(baseBranch, "/") {
			baseBranch = "origin/" + baseBranch
		}

		fmt.Printf("Creating worktree %s from %s...\n", branchName, baseBranch)
		if err := gitutil.CreateWorktreeFromRef(repoPath, worktreePath, branchName, baseBranch); err != nil {
			// Cleanup workspace on failure
			os.RemoveAll(ws.Path)
			return fmt.Errorf("create worktree for %s: %w", repoName, err)
		}

		// Copy files from repo to worktree (e.g., .env, .vscode/)
		if len(rc.CopyFiles) > 0 {
			for _, file := range rc.CopyFiles {
				src := filepath.Join(repoPath, file)
				dst := filepath.Join(worktreePath, file)
				if err := copyPath(src, dst); err != nil {
					fmt.Printf("Warning: failed to copy %s: %v\n", file, err)
				} else {
					fmt.Printf("  Copied %s\n", file)
				}
			}
		}

		// Run post-setup command if specified
		if rc.PostSetup != "" {
			fmt.Printf("Running post-setup: %s\n", rc.PostSetup)
			cmd := exec.Command("sh", "-c", rc.PostSetup)
			cmd.Dir = worktreePath
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: post-setup failed: %v\n", err)
			}
		}

		// Symlink worktree into workspace
		if err := ws.AddRepoLink(repoName, worktreePath); err != nil {
			return fmt.Errorf("link worktree: %w", err)
		}

		ws.Repos = append(ws.Repos, repoName)
	}

	// Determine persona (priority: flag > auto-select from tags > config default > builder)
	persona := pickupPersona
	if persona == "" {
		// Try auto-select from task tags
		if reg, err := personas.LoadRegistry(cfg.Home); err == nil {
			if matched := reg.SelectForTask(t.Tags); matched != nil {
				persona = matched.Name
				fmt.Printf("Auto-selected persona: %s (matched tag)\n", persona)
			}
		}
	}
	if persona == "" {
		persona = cfg.Defaults.Persona
	}
	if persona == "" {
		persona = "builder"
	}
	ws.Persona = persona

	// Load context and generate AGENTS.md (with CLAUDE.md symlink)
	notes, _ := yokeClient.GetNotes(t.ID)
	events, _ := yokeClient.GetEvents(t.ID)
	parent, _ := yokeClient.GetParent(t)
	children, _ := yokeClient.GetChildren(t.ID)
	blockers, _ := yokeClient.GetBlockers(t)

	gen := context.NewGenerator(cfg.Home)
	taskCtx := &context.TaskContext{
		Task:     t,
		Notes:    notes,
		Events:   events,
		Parent:   parent,
		Children: children,
		Blockers: blockers,
		Persona:  persona,
		Repos:    pickupRepos,
	}

	if err := gen.Generate(ws.Path, taskCtx); err != nil {
		fmt.Printf("Warning: failed to generate AGENTS.md: %v\n", err)
	}

	// Update task status in yoke to in_progress
	if t.Status != "in_progress" {
		if err := yokeClient.UpdateStatus(t.ID, "in_progress"); err != nil {
			fmt.Printf("Warning: failed to update task status: %v\n", err)
		}
	}

	fmt.Printf("\nWorkspace created: %s\n", ws.Path)
	fmt.Printf("Persona: %s\n", persona)
	fmt.Printf("Repos: %s\n", strings.Join(pickupRepos, ", "))

	// Surface relevant learnings
	surfaceRelevantLearnings(cfg.Home, t.Tags, pickupRepos)

	fmt.Println("\nNext steps:")
	fmt.Printf("  cd %s\n", ws.Path)
	fmt.Println("  # Start working with your AI agent")

	return nil
}

// surfaceRelevantLearnings shows learnings matching task tags or repos.
func surfaceRelevantLearnings(oxHome string, taskTags, repos []string) {
	store, err := learning.NewStore(oxHome)
	if err != nil {
		return
	}
	defer store.Close()

	// Combine task tags and repo names for search
	searchTags := append([]string{}, taskTags...)
	searchTags = append(searchTags, repos...)

	learnings, err := store.SearchByTags(searchTags, 5)
	if err != nil || len(learnings) == 0 {
		return
	}

	fmt.Println("\nRelevant learnings:")
	for _, l := range learnings {
		content := l.Content
		if len(content) > 60 {
			content = content[:57] + "..."
		}
		fmt.Printf("  [%s] %s\n", l.Category, content)
	}
}

// slugify converts a title to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimRight(s, "-")
	}
	return s
}

// copyPath copies a file or directory from src to dst.
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

// copyFile copies a single file.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Preserve permissions
	info, _ := os.Stat(src)
	return os.Chmod(dst, info.Mode())
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func init() {
	pickupCmd.Flags().StringSliceVarP(&pickupRepos, "repos", "r", nil, "Repos to include (required)")
	pickupCmd.Flags().StringVarP(&pickupPersona, "persona", "p", "", "Persona to use (default: builder)")
	pickupCmd.MarkFlagRequired("repos")

	rootCmd.AddCommand(pickupCmd)
}
