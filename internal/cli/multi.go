package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/filelock"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/workspace"
	"github.com/ashvinbhat/ox/internal/yokehelper"
	"github.com/spf13/cobra"
)

var (
	multiRepos    []string
	multiDryRun   bool
	multiNoTui    bool
	multiNoReview bool
)

var multiCmd = &cobra.Command{
	Use:   "multi <task-id>",
	Short: "Launch multi-agent orchestration for a task",
	Long: `Runs a captain agent to plan the work, then spawns builder agents to execute in parallel.

Flow:
1. Captain analyzes the task and codebase, produces a plan
2. Review panel (architect, guardian, pragmatist) critiques the plan in parallel
3. Captain revises the plan and produces a decision log
4. You review the plan + decisions and approve
5. Builder agents are spawned in tmux sessions
6. Use 'ox agents' to monitor, 'ox peek/msg/attach/kill' to interact

Examples:
  ox multi 18 --repos backend              # Plan and execute
  ox multi 18 --repos backend,frontend     # Multi-repo task
  ox multi 18 --repos backend --dry-run    # Plan only, don't spawn`,
	Args: cobra.ExactArgs(1),
	RunE: runMulti,
}

func runMulti(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	taskRef := args[0]

	if len(multiRepos) == 0 {
		return fmt.Errorf("at least one repo required (use --repos)")
	}

	// Validate repos
	for _, r := range multiRepos {
		if _, exists := cfg.Repos[r]; !exists {
			return fmt.Errorf("repo %q not registered (run 'ox repo list')", r)
		}
	}

	// Load task
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return fmt.Errorf("open yoke: %w", err)
	}
	defer yokeClient.Close()

	t, err := yokeClient.Get(taskRef)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	mgr := agent.NewManager(cfg.Home, cfg)

	// Create agents directory
	agentsDir := mgr.AgentsDir(t.ID)
	os.MkdirAll(agentsDir, 0o755)

	// Check for existing workspace (from ox pickup)
	existingWs, wsErr := workspace.Open(cfg.Home, taskRef)

	// Initialize or load registry
	reg, regErr := mgr.LoadRegistry(t.ID)
	if regErr != nil {
		reg, err = mgr.InitSession(t.ID, t.Title, t.Seq)
		if err != nil {
			return fmt.Errorf("init agent session: %w", err)
		}
	}

	// If existing workspace found, adopt its worktrees as integration branches
	if wsErr == nil && existingWs != nil {
		fmt.Printf("🐂 Multi-agent orchestration for task #%d: %s\n", t.Seq, t.Title)
		fmt.Printf("   Found existing workspace: %s\n", existingWs.Slug)

		if reg.IntegrationBranches == nil {
			reg.IntegrationBranches = make(map[string]string)
		}

		for _, repoName := range existingWs.Repos {
			// Resolve symlink to get the actual worktree path
			linkPath := filepath.Join(existingWs.Path, repoName)
			worktreePath, err := os.Readlink(linkPath)
			if err != nil {
				// Not a symlink, might be a real directory
				worktreePath = linkPath
			}
			// Make absolute if relative
			if !filepath.IsAbs(worktreePath) {
				worktreePath = filepath.Join(existingWs.Path, worktreePath)
			}

			// Get the branch name from the worktree
			branchName, err := gitutil.CurrentBranch(worktreePath)
			if err != nil {
				fmt.Printf("   Warning: could not detect branch for %s: %v\n", repoName, err)
				continue
			}

			reg.IntegrationBranches[repoName] = worktreePath
			if reg.IntegrationBranch == "" {
				reg.IntegrationBranch = branchName
			}

			fmt.Printf("   Using %s → %s (branch: %s)\n", repoName, worktreePath, branchName)
		}

		// Add any repos from --repos that aren't in the workspace
		for _, r := range multiRepos {
			found := false
			for _, wr := range existingWs.Repos {
				if wr == r {
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("   Note: repo %q not in existing workspace, will create new worktree\n", r)
			}
		}

		mgr.SaveRegistry(t.ID, reg)
		fmt.Println()
	} else {
		fmt.Printf("🐂 Multi-agent orchestration for task #%d: %s\n", t.Seq, t.Title)
		fmt.Println("   Creating workspace...")

		// Create workspace (same as ox pickup)
		ws, err := workspace.Create(cfg.Home, t.ID, t.Seq, t.Title)
		if err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}

		if reg.IntegrationBranches == nil {
			reg.IntegrationBranches = make(map[string]string)
		}

		for _, repoName := range multiRepos {
			rc := cfg.Repos[repoName]
			repoPath := filepath.Join(cfg.Home, "repos", repoName)

			fmt.Printf("   Fetching %s...\n", repoName)
			if err := gitutil.Fetch(repoPath); err != nil {
				return fmt.Errorf("fetch %s failed: %w", repoName, err)
			}

			branchName := fmt.Sprintf("ox/%d-%s", t.Seq, slugifyAgent(t.Title))
			worktreePath := filepath.Join(cfg.Home, "worktrees", repoName, fmt.Sprintf("%d", t.Seq))
			os.MkdirAll(filepath.Dir(worktreePath), 0o755)

			baseBranch := rc.BaseBranch
			if baseBranch == "" {
				baseBranch = "origin/main"
			}
			if !strings.HasPrefix(baseBranch, "origin/") && !strings.Contains(baseBranch, "/") {
				baseBranch = "origin/" + baseBranch
			}

			fmt.Printf("   Creating worktree %s from %s...\n", branchName, baseBranch)
			if err := gitutil.CreateWorktreeFromRef(repoPath, worktreePath, branchName, baseBranch); err != nil {
				os.RemoveAll(ws.Path)
				return fmt.Errorf("create worktree for %s: %w", repoName, err)
			}

			// Copy files if configured
			for _, file := range rc.CopyFiles {
				src := filepath.Join(repoPath, file)
				dst := filepath.Join(worktreePath, file)
				copyPath(src, dst)
			}

			// Run post-setup if configured
			if rc.PostSetup != "" {
				fmt.Printf("   Running post-setup for %s...\n", repoName)
				postCmd := exec.Command("sh", "-c", rc.PostSetup)
				postCmd.Dir = worktreePath
				postCmd.Stdout = os.Stdout
				postCmd.Stderr = os.Stderr
				postCmd.Run()
			}

			// Symlink into workspace
			ws.AddRepoLink(repoName, worktreePath)

			reg.IntegrationBranches[repoName] = worktreePath
			if reg.IntegrationBranch == "" {
				reg.IntegrationBranch = branchName
			}

			fmt.Printf("   %s → %s (branch: %s)\n", repoName, worktreePath, branchName)
		}

		mgr.SaveRegistry(t.ID, reg)
		fmt.Printf("   Workspace: %s\n\n", ws.Path)
	}

	// Step 1: Generate captain context
	fmt.Println("Step 1: Captain planning...")
	captainCtx := mgr.GenerateCaptainContext(t.Seq, t.Title, t.Body, multiRepos, agentsDir)
	agentsmdPath := filepath.Join(agentsDir, "AGENTS.md")
	if err := os.WriteFile(agentsmdPath, []byte(captainCtx), 0o644); err != nil {
		return fmt.Errorf("write captain AGENTS.md: %w", err)
	}
	// Symlink CLAUDE.md
	claudePath := filepath.Join(agentsDir, "CLAUDE.md")
	os.Remove(claudePath)
	os.Symlink("AGENTS.md", claudePath)

	// Step 2: Run captain to produce plan
	captainModel := cfg.Multi.CaptainModel
	if captainModel == "" {
		captainModel = "opus"
	}
	maxTurns := 40
	maxBudget := 20.0
	if cfg.Multi.MaxBudgetPerAgent > 0 {
		maxBudget = cfg.Multi.MaxBudgetPerAgent * 2 // Captain gets more budget
	}

	fmt.Printf("Running captain (model: %s, max-turns: %d)...\n\n", captainModel, maxTurns)

	// Build repo dirs map (worktree paths if available, base repos as fallback)
	// Re-read registry to get latest integration branches
	reg, _ = mgr.LoadRegistry(t.ID)
	repoDirs := make(map[string]string)
	if reg != nil && reg.IntegrationBranches != nil {
		for repo, path := range reg.IntegrationBranches {
			repoDirs[repo] = path
		}
	}

	if err := mgr.RunCaptainPlanning(agentsDir, multiRepos, repoDirs, captainModel, maxTurns, maxBudget); err != nil {
		fmt.Printf("\nWarning: captain exited with error: %v\n", err)
		// Continue anyway — plan.md might still have been written
	}

	// Step 3: Parse plan
	planPath := filepath.Join(agentsDir, "plan.md")
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		return fmt.Errorf("captain did not produce a plan at %s", planPath)
	}

	plan, err := agent.ParsePlan(planPath)
	if err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}

	// Display initial plan
	agent.DisplayPlan(plan)

	// Step 4: Review panel (unless --no-review)
	if !multiNoReview {
		reviewModel := cfg.Multi.DefaultModel
		if reviewModel == "" {
			reviewModel = "sonnet"
		}

		fmt.Println("Step 2: Review panel (3 reviewers in parallel)...")
		reviewResults, err := agent.RunReviewPanel(agentsDir, planPath, multiRepos, repoDirs, cfg.Home, reviewModel)
		if err != nil {
			fmt.Printf("Warning: review panel error: %v\n", err)
		} else {
			agent.DisplayReviewSummary(reviewResults)
		}

		// Step 5: Captain revision
		fmt.Println("\nStep 3: Captain revising plan based on reviews...")
		if err := agent.RunCaptainRevision(agentsDir, multiRepos, repoDirs, cfg.Home, captainModel); err != nil {
			fmt.Printf("Warning: captain revision error: %v\n", err)
		}

		// Re-parse revised plan
		plan, err = agent.ParsePlan(planPath)
		if err != nil {
			return fmt.Errorf("parse revised plan: %w", err)
		}

		// Show decision log if it exists
		decisionsPath := filepath.Join(agentsDir, "decisions.md")
		if data, err := os.ReadFile(decisionsPath); err == nil {
			fmt.Printf("\n📓 Decision Log:\n%s\n", string(data))
		}

		// Display revised plan
		fmt.Println("\n--- Revised Plan ---")
		agent.DisplayPlan(plan)
	}

	// Validate
	issues := agent.ValidatePlan(plan, multiRepos)
	if len(issues) > 0 {
		fmt.Println("\n⚠️  Plan issues:")
		for _, issue := range issues {
			fmt.Printf("  - %s\n", issue)
		}
		fmt.Println()
	}

	if multiDryRun {
		fmt.Println("Dry run — plan saved but no agents spawned.")
		fmt.Printf("Plan: %s\n", planPath)
		return nil
	}

	// Step 4: Ask for approval
	fmt.Printf("Full plan: %s\n", planPath)
	if _, err := os.Stat(filepath.Join(agentsDir, "decisions.md")); err == nil {
		fmt.Printf("Decisions: %s\n", filepath.Join(agentsDir, "decisions.md"))
	}
	fmt.Print("\nProceed with spawning agents? [y/n/edit] ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	switch input {
	case "y", "yes":
		// Continue
	case "edit":
		fmt.Printf("\nEdit the plan at: %s\n", planPath)
		fmt.Println("Then run: ox multi", taskRef, "--repos", strings.Join(multiRepos, ","), "--from-plan")
		return nil
	default:
		fmt.Println("Aborted.")
		return nil
	}

	// Step 5: Create integration worktrees
	fmt.Println("\nCreating integration branch...")
	if err := mgr.CreateIntegrationWorktrees(t.ID, t.Seq, t.Title, multiRepos); err != nil {
		return fmt.Errorf("create integration worktrees: %w", err)
	}

	// Step 6: Spawn builders
	if err := spawnFromPlan(mgr, cfg, t.ID, t.Seq, plan, agentsDir); err != nil {
		return err
	}

	// Step 6: Launch TUI unless --no-tui
	if !multiNoTui {
		return LaunchTUI(mgr, t.ID)
	}
	return nil
}

func spawnFromPlan(mgr *agent.Manager, cfg *config.Config, taskID string, taskSeq int, plan *agent.Plan, agentsDir string) error {
	lockMgr := filelock.NewManager(agentsDir)

	fmt.Printf("\nSpawning %d agents...\n\n", len(plan.Agents))

	// Determine which agents can start immediately (no dependencies)
	for _, pa := range plan.Agents {
		// Check if dependencies are met (for now, skip agents with deps on unfinished agents)
		canStart := true
		for _, dep := range pa.DependsOn {
			// Check if dependency agent is done
			depAgent, _ := mgr.GetAgent(taskID, dep)
			if depAgent == nil || depAgent.Status != agent.StatusDone {
				canStart = false
				break
			}
		}

		a := &agent.Agent{
			ID:          pa.ID,
			TaskID:      taskID,
			TaskSeq:     taskSeq,
			SubtaskDesc: pa.Description,
			Persona:     pa.Persona,
			Model:       pa.Model,
			Repo:        pa.Repo,
			FileLocks:   pa.Files,
		}

		// Apply config defaults
		if a.Model == "" && cfg.Multi.DefaultModel != "" {
			a.Model = cfg.Multi.DefaultModel
		}
		if cfg.Multi.DefaultMaxTurns > 0 {
			a.MaxTurns = cfg.Multi.DefaultMaxTurns
		}
		if cfg.Multi.MaxBudgetPerAgent > 0 {
			a.MaxBudget = cfg.Multi.MaxBudgetPerAgent
		}

		// Acquire file locks
		if len(pa.Files) > 0 {
			if err := lockMgr.Acquire(pa.ID, pa.Files); err != nil {
				fmt.Printf("  ⚠️  %s: lock conflict: %v (skipped)\n", pa.ID, err)
				continue
			}
		}

		if !canStart {
			a.Status = agent.StatusPending
			mgr.RegisterAgent(taskID, a)
			fmt.Printf("  ○ %s: queued (waiting for %s)\n", pa.ID, strings.Join(pa.DependsOn, ", "))
			continue
		}

		if err := mgr.SpawnAgent(taskID, taskSeq, a); err != nil {
			fmt.Printf("  ✗ %s: spawn failed: %v\n", pa.ID, err)
			continue
		}
		fmt.Printf("  ● %s: spawned (%s in %s)\n", pa.ID, a.Persona, a.Repo)
	}

	fmt.Println("\nAgents running. Use these commands to manage:")
	fmt.Println("  ox agents       # list all agents")
	fmt.Println("  ox peek <id>    # view agent output")
	fmt.Println("  ox msg <id> ..  # send message")
	fmt.Println("  ox attach <id>  # take over session")
	fmt.Println("  ox kill <id>    # terminate agent")

	return nil
}

func init() {
	multiCmd.Flags().StringSliceVar(&multiRepos, "repos", nil, "Repos to include (required)")
	multiCmd.Flags().BoolVar(&multiDryRun, "dry-run", false, "Plan only, don't spawn agents")
	multiCmd.Flags().BoolVar(&multiNoTui, "no-tui", false, "Skip TUI after spawning")
	multiCmd.Flags().BoolVar(&multiNoReview, "no-review", false, "Skip review panel")
	multiCmd.MarkFlagRequired("repos")

	rootCmd.AddCommand(multiCmd)
}
