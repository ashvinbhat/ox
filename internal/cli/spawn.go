package cli

import (
	"fmt"
	"strings"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/yokehelper"
	"github.com/spf13/cobra"
)

var (
	spawnTask     string
	spawnRepo     string
	spawnPersona  string
	spawnModel    string
	spawnMaxTurns int
	spawnBudget   float64
	spawnFiles    []string
)

var spawnCmd = &cobra.Command{
	Use:   "spawn <subtask-description>",
	Short: "Spawn a new agent in a tmux session",
	Long: `Creates a new AI agent that works on a subtask in its own git worktree and tmux session.

Examples:
  ox spawn "implement JWT auth middleware" --task 18 --repo backend
  ox spawn "write unit tests" --task 18 --repo backend --persona reviewer --model haiku
  ox spawn "build login form" --task 18 --repo frontend --files "src/components/Auth/**"`,
	Args: cobra.ExactArgs(1),
	RunE: runSpawn,
}

func runSpawn(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	subtaskDesc := args[0]

	if spawnTask == "" {
		return fmt.Errorf("--task is required")
	}
	if spawnRepo == "" {
		return fmt.Errorf("--repo is required")
	}

	// Validate repo
	if _, exists := cfg.Repos[spawnRepo]; !exists {
		return fmt.Errorf("repo %q not registered (run 'ox repo list')", spawnRepo)
	}

	// Load task from yoke to get seq
	yokeClient, err := yokehelper.NewClient()
	if err != nil {
		return fmt.Errorf("open yoke: %w", err)
	}
	defer yokeClient.Close()

	t, err := yokeClient.Get(spawnTask)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	// Derive agent ID from subtask description
	agentID := slugifyAgent(subtaskDesc)

	// Apply defaults from config
	persona := spawnPersona
	if persona == "" {
		persona = "builder"
	}
	model := spawnModel
	if model == "" && cfg.Multi.DefaultModel != "" {
		model = cfg.Multi.DefaultModel
	}
	maxTurns := spawnMaxTurns
	if maxTurns == 0 && cfg.Multi.DefaultMaxTurns > 0 {
		maxTurns = cfg.Multi.DefaultMaxTurns
	}
	budget := spawnBudget
	if budget == 0 && cfg.Multi.MaxBudgetPerAgent > 0 {
		budget = cfg.Multi.MaxBudgetPerAgent
	}

	mgr := agent.NewManager(cfg.Home, cfg)

	// Ensure agent session exists for this task
	if _, err := mgr.LoadRegistry(t.ID); err != nil {
		if _, err := mgr.InitSession(t.ID, t.Title, t.Seq); err != nil {
			return fmt.Errorf("init agent session: %w", err)
		}
	}

	a := &agent.Agent{
		ID:          agentID,
		TaskID:      t.ID,
		TaskSeq:     t.Seq,
		SubtaskDesc: subtaskDesc,
		Persona:     persona,
		Model:       model,
		Repo:        spawnRepo,
		FileLocks:   spawnFiles,
		MaxTurns:    maxTurns,
		MaxBudget:   budget,
	}

	fmt.Printf("Spawning agent %q for task #%d...\n", agentID, t.Seq)
	fmt.Printf("  Repo: %s\n", spawnRepo)
	fmt.Printf("  Persona: %s\n", persona)
	if model != "" {
		fmt.Printf("  Model: %s\n", model)
	}

	if err := mgr.SpawnAgent(t.ID, t.Seq, a); err != nil {
		return fmt.Errorf("spawn agent: %w", err)
	}

	fmt.Printf("\nAgent spawned: %s\n", a.TmuxSession)
	fmt.Printf("  Worktree: %s\n", a.WorktreePath)
	fmt.Printf("  Branch: %s\n", a.BranchName)
	fmt.Println("\nCommands:")
	fmt.Printf("  ox peek %s      # view output\n", agentID)
	fmt.Printf("  ox attach %s    # take over session\n", agentID)
	fmt.Printf("  ox msg %s \"..\"  # send message\n", agentID)
	fmt.Printf("  ox kill %s      # terminate\n", agentID)

	return nil
}

func slugifyAgent(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
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

func init() {
	spawnCmd.Flags().StringVar(&spawnTask, "task", "", "Task ID or sequence number (required)")
	spawnCmd.Flags().StringVar(&spawnRepo, "repo", "", "Repository to work in (required)")
	spawnCmd.Flags().StringVar(&spawnPersona, "persona", "", "Persona (default: builder)")
	spawnCmd.Flags().StringVar(&spawnModel, "model", "", "Claude model (e.g. opus, sonnet, haiku)")
	spawnCmd.Flags().IntVar(&spawnMaxTurns, "max-turns", 0, "Maximum conversation turns")
	spawnCmd.Flags().Float64Var(&spawnBudget, "max-budget", 0, "Maximum budget in USD")
	spawnCmd.Flags().StringSliceVar(&spawnFiles, "files", nil, "File patterns this agent owns (glob)")
	spawnCmd.MarkFlagRequired("task")
	spawnCmd.MarkFlagRequired("repo")

	rootCmd.AddCommand(spawnCmd)
}
