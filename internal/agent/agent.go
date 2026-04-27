package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

// AgentStatus represents the current state of an agent.
type AgentStatus string

const (
	StatusPending AgentStatus = "pending"
	StatusRunning AgentStatus = "running"
	StatusIdle    AgentStatus = "idle"
	StatusDone    AgentStatus = "done"
	StatusFailed  AgentStatus = "failed"
	StatusKilled  AgentStatus = "killed"
)

// Agent represents a single AI agent working on a subtask.
type Agent struct {
	ID           string      `json:"id"`
	TaskID       string      `json:"task_id"`
	TaskSeq      int         `json:"task_seq"`
	SubtaskDesc  string      `json:"subtask_desc"`
	Persona      string      `json:"persona"`
	Model        string      `json:"model"`
	Repo         string      `json:"repo"`
	Status       AgentStatus `json:"status"`
	TmuxSession  string      `json:"tmux_session"`
	WorktreePath string      `json:"worktree_path"`
	BranchName   string      `json:"branch_name"`
	FileLocks    []string    `json:"file_locks,omitempty"`
	SpawnedAt    time.Time   `json:"spawned_at"`
	FinishedAt   *time.Time  `json:"finished_at,omitempty"`
	MaxTurns     int         `json:"max_turns,omitempty"`
	MaxBudget    float64     `json:"max_budget_usd,omitempty"`
}

// AgentRegistry tracks all agents for a given task.
type AgentRegistry struct {
	TaskID              string            `json:"task_id"`
	TaskTitle           string            `json:"task_title"`
	TaskSeq             int               `json:"task_seq"`
	CreatedAt           time.Time         `json:"created_at"`
	Agents              []*Agent          `json:"agents"`
	Captain             string            `json:"captain,omitempty"`
	IntegrationBranches map[string]string `json:"integration_branches,omitempty"` // repo → worktree path
	IntegrationBranch   string            `json:"integration_branch,omitempty"`   // branch name (e.g., ox/18-jwt-auth)
}

// Manager manages agent lifecycle.
type Manager struct {
	oxHome string
	cfg    *config.Config
}

// NewManager creates a new agent manager.
func NewManager(oxHome string, cfg *config.Config) *Manager {
	return &Manager{
		oxHome: oxHome,
		cfg:    cfg,
	}
}

// AgentsBaseDir returns the base agents directory.
func (m *Manager) AgentsBaseDir() string {
	return filepath.Join(m.oxHome, "agents")
}

// AgentsDir returns the agents directory for a specific task.
func (m *Manager) AgentsDir(taskID string) string {
	return filepath.Join(m.oxHome, "agents", taskID)
}

// InitSession creates the agents directory and initial registry for a task.
func (m *Manager) InitSession(taskID, taskTitle string, taskSeq int) (*AgentRegistry, error) {
	dir := m.AgentsDir(taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create agents dir: %w", err)
	}

	reg := &AgentRegistry{
		TaskID:    taskID,
		TaskTitle: taskTitle,
		TaskSeq:   taskSeq,
		CreatedAt: time.Now(),
		Agents:    []*Agent{},
	}

	if err := m.SaveRegistry(taskID, reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// LoadRegistry loads the agent registry for a task.
func (m *Manager) LoadRegistry(taskID string) (*AgentRegistry, error) {
	path := filepath.Join(m.AgentsDir(taskID), "agents.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agents.json: %w", err)
	}

	var reg AgentRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse agents.json: %w", err)
	}
	return &reg, nil
}

// SaveRegistry writes the agent registry to disk.
func (m *Manager) SaveRegistry(taskID string, reg *AgentRegistry) error {
	path := filepath.Join(m.AgentsDir(taskID), "agents.json")
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agents.json: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// RegisterAgent adds an agent to the registry.
func (m *Manager) RegisterAgent(taskID string, agent *Agent) error {
	reg, err := m.LoadRegistry(taskID)
	if err != nil {
		return err
	}

	reg.Agents = append(reg.Agents, agent)
	return m.SaveRegistry(taskID, reg)
}

// UpdateAgent atomically updates an agent in the registry.
func (m *Manager) UpdateAgent(taskID, agentID string, fn func(*Agent)) error {
	reg, err := m.LoadRegistry(taskID)
	if err != nil {
		return err
	}

	for _, a := range reg.Agents {
		if a.ID == agentID {
			fn(a)
			return m.SaveRegistry(taskID, reg)
		}
	}
	return fmt.Errorf("agent %q not found", agentID)
}

// GetAgent returns an agent by ID from a task registry.
func (m *Manager) GetAgent(taskID, agentID string) (*Agent, error) {
	reg, err := m.LoadRegistry(taskID)
	if err != nil {
		return nil, err
	}

	for _, a := range reg.Agents {
		if a.ID == agentID {
			return a, nil
		}
	}
	return nil, fmt.Errorf("agent %q not found in task %s", agentID, taskID)
}

// FindAgent searches all task registries for an agent by ID.
func (m *Manager) FindAgent(agentID string) (*Agent, string, error) {
	baseDir := m.AgentsBaseDir()
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, "", fmt.Errorf("read agents dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID := entry.Name()
		reg, err := m.LoadRegistry(taskID)
		if err != nil {
			continue
		}
		for _, a := range reg.Agents {
			if a.ID == agentID {
				return a, taskID, nil
			}
		}
	}
	return nil, "", fmt.Errorf("agent %q not found", agentID)
}

// ListAllRegistries returns all agent registries.
func (m *Manager) ListAllRegistries() ([]*AgentRegistry, error) {
	baseDir := m.AgentsBaseDir()
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var registries []*AgentRegistry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		reg, err := m.LoadRegistry(entry.Name())
		if err != nil {
			continue
		}
		registries = append(registries, reg)
	}
	return registries, nil
}

// ReconcileStatus checks tmux sessions and updates agent statuses.
// An agent is considered done if its tmux session is gone OR if Claude
// is no longer running inside the session (shell prompt visible).
func (m *Manager) ReconcileStatus(taskID string) error {
	reg, err := m.LoadRegistry(taskID)
	if err != nil {
		return err
	}

	changed := false
	for _, a := range reg.Agents {
		if a.Status != StatusRunning {
			continue
		}

		isDone := false
		if !tmuxutil.HasSession(a.TmuxSession) {
			isDone = true
		} else {
			// Session exists — check if Claude is still running
			// If the pane shows a shell prompt (➜ or $) at the bottom, Claude has exited
			output, err := tmuxutil.CapturePane(a.TmuxSession, 5)
			if err == nil && isClaudeDone(output) {
				isDone = true
			}
		}

		if isDone {
			now := time.Now()
			a.Status = StatusDone
			a.FinishedAt = &now
			changed = true
		}
	}

	if changed {
		return m.SaveRegistry(taskID, reg)
	}
	return nil
}

// isClaudeDone checks if Claude has exited by looking for shell prompt patterns
// at the bottom of the captured pane output.
func isClaudeDone(output string) bool {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return false
	}
	// Check last few non-empty lines for shell prompt indicators
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// If line contains Claude Code UI elements, it's still running
		if strings.Contains(line, "bypass permissions") || strings.Contains(line, "Claude Code") ||
			strings.Contains(line, "shift+tab to cycle") {
			return false
		}
		// Shell prompt patterns (zsh/bash): ends with ➜, $, %, or contains them as prompts
		if strings.HasSuffix(line, "➜") || strings.HasSuffix(line, "$ ") ||
			strings.HasSuffix(line, "% ") || strings.HasSuffix(line, "#") {
			return true
		}
		// zsh prompt like "dirname ➜" or with git branch
		if strings.Contains(line, "➜") && !strings.Contains(line, "claude") {
			return true
		}
	}
	return false
}

// CreateIntegrationWorktrees creates worktrees for the integration branch in each repo.
// This is the branch that agent work gets merged into, and ultimately becomes the PR branch.
func (m *Manager) CreateIntegrationWorktrees(taskID string, taskSeq int, taskTitle string, repos []string) error {
	reg, err := m.LoadRegistry(taskID)
	if err != nil {
		return err
	}

	if reg.IntegrationBranches == nil {
		reg.IntegrationBranches = make(map[string]string)
	}

	slug := slugify(taskTitle)
	branchName := fmt.Sprintf("ox/%d-%s", taskSeq, slug)
	reg.IntegrationBranch = branchName

	for _, repoName := range repos {
		// Skip repos that already have integration branches (from existing workspace)
		if _, exists := reg.IntegrationBranches[repoName]; exists {
			fmt.Printf("  Integration worktree: %s → %s (existing)\n", repoName, reg.IntegrationBranch)
			continue
		}

		rc, exists := m.cfg.Repos[repoName]
		if !exists {
			continue
		}
		repoPath := filepath.Join(m.oxHome, "repos", repoName)

		// Fetch latest
		if err := gitutil.Fetch(repoPath); err != nil {
			return fmt.Errorf("fetch %s: %w", repoName, err)
		}

		// Create integration worktree
		worktreeDir := filepath.Join(m.oxHome, "worktrees", repoName, fmt.Sprintf("%d-integration", taskSeq))
		os.MkdirAll(filepath.Dir(worktreeDir), 0o755)

		baseBranch := rc.BaseBranch
		if baseBranch == "" {
			baseBranch = "origin/main"
		}
		if !strings.HasPrefix(baseBranch, "origin/") && !strings.Contains(baseBranch, "/") {
			baseBranch = "origin/" + baseBranch
		}

		if err := gitutil.CreateWorktreeFromRef(repoPath, worktreeDir, branchName, baseBranch); err != nil {
			return fmt.Errorf("create integration worktree for %s: %w", repoName, err)
		}

		reg.IntegrationBranches[repoName] = worktreeDir
		fmt.Printf("  Integration worktree: %s → %s (new)\n", repoName, branchName)
	}

	return m.SaveRegistry(taskID, reg)
}

// slugify converts a title to a short kebab-case slug.
func slugify(s string) string {
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

// SpawnAgent creates a worktree, generates context, starts a tmux session, and registers the agent.
func (m *Manager) SpawnAgent(taskID string, taskSeq int, agent *Agent) error {
	if !tmuxutil.IsAvailable() {
		return fmt.Errorf("tmux is not installed or not in PATH")
	}

	// Resolve repo path
	rc, exists := m.cfg.Repos[agent.Repo]
	if !exists {
		return fmt.Errorf("repo %q not registered", agent.Repo)
	}
	repoPath := filepath.Join(m.oxHome, "repos", agent.Repo)

	// Fetch latest
	if err := gitutil.Fetch(repoPath); err != nil {
		return fmt.Errorf("fetch %s: %w", agent.Repo, err)
	}

	// Create worktree
	worktreeDir := filepath.Join(m.oxHome, "worktrees", agent.Repo, fmt.Sprintf("%d-%s", taskSeq, agent.ID))
	os.MkdirAll(filepath.Dir(worktreeDir), 0o755)

	branchName := fmt.Sprintf("ox/%d-%s", taskSeq, agent.ID)
	baseBranch := rc.BaseBranch
	if baseBranch == "" {
		baseBranch = "origin/main"
	}
	if !strings.HasPrefix(baseBranch, "origin/") && !strings.Contains(baseBranch, "/") {
		baseBranch = "origin/" + baseBranch
	}

	if err := gitutil.CreateWorktreeFromRef(repoPath, worktreeDir, branchName, baseBranch); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}

	agent.WorktreePath = worktreeDir
	agent.BranchName = branchName
	agent.TmuxSession = fmt.Sprintf("ox-%d-%s", taskSeq, agent.ID)
	agent.SpawnedAt = time.Now()
	agent.Status = StatusRunning

	// Write subtask description
	agentDir := filepath.Join(m.AgentsDir(taskID), agent.ID)
	os.MkdirAll(agentDir, 0o755)
	taskMd := fmt.Sprintf("# Subtask: %s\n\n%s\n", agent.ID, agent.SubtaskDesc)
	os.WriteFile(filepath.Join(agentDir, "task.md"), []byte(taskMd), 0o644)

	// Generate builder AGENTS.md in worktree
	agentsMd := m.generateBuilderContext(agent, taskID)
	os.WriteFile(filepath.Join(worktreeDir, "AGENTS.md"), []byte(agentsMd), 0o644)
	// Symlink CLAUDE.md -> AGENTS.md
	claudePath := filepath.Join(worktreeDir, "CLAUDE.md")
	os.Remove(claudePath)
	os.Symlink("AGENTS.md", claudePath)

	// Copy files if configured
	if len(rc.CopyFiles) > 0 {
		for _, file := range rc.CopyFiles {
			src := filepath.Join(repoPath, file)
			dst := filepath.Join(worktreeDir, file)
			copyFile(src, dst)
		}
	}

	// Create tmux session
	if err := tmuxutil.NewSession(agent.TmuxSession, worktreeDir); err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}

	// Set environment variables
	tmuxutil.SetEnv(agent.TmuxSession, "OX_AGENT_ID", agent.ID)
	tmuxutil.SetEnv(agent.TmuxSession, "OX_TASK_ID", taskID)

	// Build claude command
	claudeCmd := m.buildClaudeCmd(agent)

	// Send claude command to tmux
	if err := tmuxutil.SendKeys(agent.TmuxSession, claudeCmd); err != nil {
		tmuxutil.KillSession(agent.TmuxSession)
		return fmt.Errorf("launch claude: %w", err)
	}

	// Wait for Claude to be ready, then send initial instruction
	go func() {
		kickMsg := fmt.Sprintf("You are agent '%s'. Read AGENTS.md for your full subtask. BEGIN WORKING IMMEDIATELY. Implement your subtask, commit your changes, write your output summary. When completely done, run /exit to close this session. Do not ask for confirmation.", agent.ID)
		waitAndSendKick(agent.TmuxSession, kickMsg)
	}()

	// Register in agents.json
	return m.RegisterAgent(taskID, agent)
}

// RespawnAgent restarts a dead agent in its existing worktree.
// The worktree, branch, and any commits are preserved — only the tmux session and Claude are recreated.
func (m *Manager) RespawnAgent(taskID string, agent *Agent) error {
	if !tmuxutil.IsAvailable() {
		return fmt.Errorf("tmux is not installed or not in PATH")
	}

	if agent.WorktreePath == "" {
		return fmt.Errorf("agent %q has no worktree path", agent.ID)
	}

	// Verify worktree still exists
	if _, err := os.Stat(agent.WorktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree no longer exists at %s", agent.WorktreePath)
	}

	// Kill existing session if somehow still around
	if tmuxutil.HasSession(agent.TmuxSession) {
		tmuxutil.KillSession(agent.TmuxSession)
	}

	// Create new tmux session in the existing worktree
	if err := tmuxutil.NewSession(agent.TmuxSession, agent.WorktreePath); err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}

	// Set environment variables
	tmuxutil.SetEnv(agent.TmuxSession, "OX_AGENT_ID", agent.ID)
	tmuxutil.SetEnv(agent.TmuxSession, "OX_TASK_ID", taskID)

	// Build and send claude command
	claudeCmd := m.buildClaudeCmd(agent)
	if err := tmuxutil.SendKeys(agent.TmuxSession, claudeCmd); err != nil {
		tmuxutil.KillSession(agent.TmuxSession)
		return fmt.Errorf("launch claude: %w", err)
	}

	// Update agent status
	m.UpdateAgent(taskID, agent.ID, func(a *Agent) {
		a.Status = StatusRunning
		a.SpawnedAt = time.Now()
		a.FinishedAt = nil
	})

	// Wait for Claude to be ready, then send instruction
	go func() {
		kickMsg := fmt.Sprintf("You are agent '%s'. Read AGENTS.md for your subtask. Check git log — some work may already be done. Continue from where the previous session left off. When completely done, run /exit to close this session. BEGIN IMMEDIATELY.", agent.ID)
		waitAndSendKick(agent.TmuxSession, kickMsg)
	}()

	return nil
}

func (m *Manager) buildClaudeCmd(agent *Agent) string {
	parts := []string{"claude"}
	parts = append(parts, "--dangerously-skip-permissions")

	if agent.Model != "" {
		parts = append(parts, "--model", agent.Model)
	}
	if agent.MaxTurns > 0 {
		parts = append(parts, "--max-turns", fmt.Sprintf("%d", agent.MaxTurns))
	}
	if agent.MaxBudget > 0 {
		parts = append(parts, "--max-budget-usd", fmt.Sprintf("%.2f", agent.MaxBudget))
	}

	return strings.Join(parts, " ")
}

func (m *Manager) generateBuilderContext(agent *Agent, taskID string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Agent: %s\n\n", agent.ID))
	sb.WriteString(fmt.Sprintf("## Subtask\n%s\n\n", agent.SubtaskDesc))

	sb.WriteString(fmt.Sprintf("## Role\nPersona: %s\n", agent.Persona))
	sb.WriteString(fmt.Sprintf("Repo: %s\n\n", agent.Repo))

	if len(agent.FileLocks) > 0 {
		sb.WriteString("## File Ownership\n")
		sb.WriteString("You are responsible for these files. Do NOT modify files outside your ownership:\n")
		for _, pattern := range agent.FileLocks {
			sb.WriteString(fmt.Sprintf("- `%s`\n", pattern))
		}
		sb.WriteString("\n")
	}

	// Scratchpad reference
	scratchpadPath := filepath.Join(m.AgentsDir(taskID), "scratchpad.md")
	sb.WriteString("## Communication\n")
	sb.WriteString(fmt.Sprintf("Shared scratchpad: %s\n", scratchpadPath))
	sb.WriteString("Read it for discoveries from other agents. Write to it with:\n")
	sb.WriteString(fmt.Sprintf("  echo '### [%s] %s' >> %s\n\n", time.Now().Format("2006-01-02 15:04"), agent.ID, scratchpadPath))

	// Output instructions
	outputPath := filepath.Join(m.AgentsDir(taskID), agent.ID, "output.md")
	sb.WriteString("## When Done\n")
	sb.WriteString(fmt.Sprintf("Write a summary of your work to: %s\n", outputPath))
	sb.WriteString("Include: what you implemented, files changed, any issues encountered.\n\n")

	sb.WriteString("## Guidelines\n")
	sb.WriteString("- Follow existing patterns in the codebase\n")
	sb.WriteString("- Write tests for new functionality\n")
	sb.WriteString("- Commit your changes to this branch\n")
	sb.WriteString("- Stay focused on your subtask\n")

	sb.WriteString(fmt.Sprintf("\n---\nGenerated by ox at %s\n", time.Now().Format(time.RFC3339)))

	return sb.String()
}

// waitAndSendKick polls the tmux pane until Claude Code's prompt (❯) appears, then sends the kick message.
func waitAndSendKick(session, message string) {
	// Poll every 3 seconds for up to 90 seconds
	for i := 0; i < 30; i++ {
		time.Sleep(3 * time.Second)
		if !tmuxutil.HasSession(session) {
			return
		}
		output, err := tmuxutil.CapturePane(session, 20)
		if err != nil {
			continue
		}
		// Look for Claude Code's prompt indicator
		if strings.Contains(output, "❯") || strings.Contains(output, ">") {
			// Small extra delay to make sure prompt is fully ready
			time.Sleep(2 * time.Second)
			tmuxutil.SendKeys(session, message)
			return
		}
	}
	// Fallback: send anyway after timeout
	if tmuxutil.HasSession(session) {
		tmuxutil.SendKeys(session, message)
	}
}

// copyFile copies a single file, ignoring errors silently.
func copyFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst, data, 0o644)
}
