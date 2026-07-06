package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/filelock"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/personas"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

const (
	WorkerPending     = "pending"
	WorkerRunning     = "running"
	WorkerBlocked     = "blocked"
	WorkerInterrupted = "interrupted"
	WorkerDone        = "done"
	WorkerFailed      = "failed"
	WorkerKilled      = "killed"
)

// Worker is one session agent of a mission. SessionIDs accumulates the claude
// session per spawn/respawn — the newest is the resume handle.
type Worker struct {
	ID           string     `json:"id"`
	MissionID    string     `json:"mission_id"`
	Persona      string     `json:"persona"`
	Model        string     `json:"model"`
	Repo         string     `json:"repo"`
	Status       string     `json:"status"`
	TmuxSession  string     `json:"tmux_session"`
	WorktreePath string     `json:"worktree_path"`
	BranchName   string     `json:"branch_name"`
	SessionIDs   []string   `json:"session_ids"`
	Files        []string   `json:"files,omitempty"`
	DependsOn    []string   `json:"depends_on,omitempty"`
	MaxTurns     int        `json:"max_turns,omitempty"`
	MaxBudgetUSD float64    `json:"max_budget_usd,omitempty"`
	SpendUSD     float64    `json:"spend_usd"`
	SpawnedAt    time.Time  `json:"spawned_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Summary      string     `json:"summary,omitempty"`
}

func (w *Worker) Finished() bool {
	return w.Status == WorkerDone || w.Status == WorkerFailed || w.Status == WorkerKilled
}

func (w *Worker) LastSessionID() string {
	if len(w.SessionIDs) == 0 {
		return ""
	}
	return w.SessionIDs[len(w.SessionIDs)-1]
}

type Registry struct {
	Workers map[string]*Worker `json:"workers"`
}

func registryPath(m *mission.Mission) string { return filepath.Join(m.Dir(), "agents.json") }

func LoadRegistry(m *mission.Mission) (*Registry, error) {
	data, err := os.ReadFile(registryPath(m))
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Workers: map[string]*Worker{}}, nil
		}
		return nil, err
	}
	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse agents.json: %w", err)
	}
	if reg.Workers == nil {
		reg.Workers = map[string]*Worker{}
	}
	return &reg, nil
}

func saveRegistry(m *mission.Mission, reg *Registry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := registryPath(m) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, registryPath(m))
}

// UpdateRegistry applies fn to the registry under the mission lock.
func UpdateRegistry(oxHome string, m *mission.Mission, fn func(*Registry) error) error {
	return mission.WithLock(oxHome, m.ID, func() error {
		reg, err := LoadRegistry(m)
		if err != nil {
			return err
		}
		if err := fn(reg); err != nil {
			return err
		}
		return saveRegistry(m, reg)
	})
}

type SpawnInput struct {
	ID           string
	Repo         string
	Brief        string
	Persona      string
	Model        string
	Files        []string
	DependsOn    []string
	MaxTurns     int
	MaxBudgetUSD float64
}

// SpawnWorker registers a worker and, unless dependencies gate it, creates its
// worktree and launches its claude session. Pending workers are spawned by the
// watcher when their dependencies finish.
func SpawnWorker(cfg *config.Config, m *mission.Mission, in SpawnInput) (*Worker, error) {
	if in.ID == "" || in.Repo == "" || in.Brief == "" {
		return nil, fmt.Errorf("id, repo, and brief are required")
	}
	if _, ok := cfg.Repos[in.Repo]; !ok {
		return nil, fmt.Errorf("repo %q not registered", in.Repo)
	}
	if m.SpendFrozen {
		return nil, fmt.Errorf("mission spend is frozen — raise the budget first")
	}

	if in.Persona == "" {
		in.Persona = "builder"
	}
	if in.Model == "" {
		in.Model = personaModel(cfg, in.Persona, cfg.SessionModel())
	}
	if in.MaxTurns == 0 {
		in.MaxTurns = cfg.Multi.DefaultMaxTurns
	}
	if in.MaxBudgetUSD == 0 {
		in.MaxBudgetUSD = m.Budgets.PerAgentUSD
	}

	w := &Worker{
		ID: in.ID, MissionID: m.ID, Persona: in.Persona, Model: in.Model, Repo: in.Repo,
		Status: WorkerPending, Files: in.Files, DependsOn: in.DependsOn,
		MaxTurns: in.MaxTurns, MaxBudgetUSD: in.MaxBudgetUSD,
		TmuxSession:  fmt.Sprintf("ox-%s-%s", m.ID, in.ID),
		WorktreePath: filepath.Join(cfg.Home, "worktrees", in.Repo, fmt.Sprintf("%s-%s", m.ID, in.ID)),
		BranchName:   fmt.Sprintf("ox/%s-%s", m.ID, in.ID),
		SpawnedAt:    time.Now(),
	}

	if err := UpdateRegistry(cfg.Home, m, func(reg *Registry) error {
		if _, exists := reg.Workers[w.ID]; exists {
			return fmt.Errorf("worker %q already exists", w.ID)
		}
		if maxPar := m.Approvals.MaxParallelAgents; maxPar > 0 {
			running := 0
			for _, other := range reg.Workers {
				if other.Status == WorkerRunning || other.Status == WorkerBlocked {
					running++
				}
			}
			if running >= maxPar {
				return fmt.Errorf("max parallel agents (%d) reached — kill one or raise the limit", maxPar)
			}
		}
		if len(in.Files) > 0 {
			lockMgr := filelock.NewManager(m.Dir())
			if err := lockMgr.Acquire(w.ID, in.Files); err != nil {
				return err
			}
		}
		reg.Workers[w.ID] = w
		return nil
	}); err != nil {
		return nil, err
	}

	if err := os.WriteFile(workerFile(m, w.ID, "brief.md"), []byte(in.Brief), 0o644); err != nil {
		return nil, err
	}

	if unmet := unmetDeps(m, w); len(unmet) > 0 {
		m.AppendEvent("agent_spawned", "orchestrator", map[string]any{"id": w.ID, "pending_on": unmet})
		return w, nil
	}
	if err := StartWorker(cfg, m, w); err != nil {
		return nil, err
	}
	return w, nil
}

// StartWorker materializes worktree + tmux + claude for a registered worker.
// Also the path the watcher uses when dependencies unblock.
func StartWorker(cfg *config.Config, m *mission.Mission, w *Worker) error {
	if err := ensureWorktree(cfg, w); err != nil {
		return err
	}
	if err := writeWorkerFiles(cfg, m, w); err != nil {
		return err
	}

	sessionID := uuid.NewString()

	if tmuxutil.HasSession(w.TmuxSession) {
		tmuxutil.KillSession(w.TmuxSession)
	}
	if err := tmuxutil.NewSession(w.TmuxSession, w.WorktreePath); err != nil {
		return fmt.Errorf("worker tmux session: %w", err)
	}
	tmuxutil.SetEnv(w.TmuxSession, "OX_MISSION_ID", m.ID)
	tmuxutil.SetEnv(w.TmuxSession, "OX_AGENT_ID", w.ID)

	if err := tmuxutil.SendKeys(w.TmuxSession, workerClaudeCmd(m, w, sessionID, "")); err != nil {
		return fmt.Errorf("launch worker claude: %w", err)
	}

	if err := UpdateRegistry(cfg.Home, m, func(reg *Registry) error {
		cur := reg.Workers[w.ID]
		if cur == nil {
			return fmt.Errorf("worker %q vanished from registry", w.ID)
		}
		cur.Status = WorkerRunning
		cur.SessionIDs = append(cur.SessionIDs, sessionID)
		cur.FinishedAt = nil
		*w = *cur
		return nil
	}); err != nil {
		return err
	}

	go kickWorker(w.TmuxSession,
		fmt.Sprintf("You are worker '%s'. Read AGENTS.md for your brief, then BEGIN IMMEDIATELY. When completely done: commit, call report_done, then /exit. Do not ask for confirmation.", w.ID))

	m.AppendEvent("agent_started", "system", map[string]any{"id": w.ID, "session": sessionID})
	return nil
}

// RespawnWorker restarts a dead worker RESUME-FIRST: the previous claude
// conversation is restored in the surviving worktree; a fresh session with a
// kick message is only the fallback when no transcript exists. A lingering
// tmux session with a dead claude (worker ran /exit, shell survived) is
// reused rather than treated as "already running".
func RespawnWorker(cfg *config.Config, m *mission.Mission, w *Worker, extraContext string) error {
	sessionAlive := tmuxutil.HasSession(w.TmuxSession)
	if sessionAlive && ClaudeAlive(w.TmuxSession) {
		return fmt.Errorf("worker %q is already running", w.ID)
	}
	if _, err := os.Stat(w.WorktreePath); err != nil {
		return fmt.Errorf("worktree gone (%s) — spawn a fresh worker instead", w.WorktreePath)
	}

	prev := w.LastSessionID()
	fresh := uuid.NewString()

	if !sessionAlive {
		if err := tmuxutil.NewSession(w.TmuxSession, w.WorktreePath); err != nil {
			return fmt.Errorf("worker tmux session: %w", err)
		}
		tmuxutil.SetEnv(w.TmuxSession, "OX_MISSION_ID", m.ID)
		tmuxutil.SetEnv(w.TmuxSession, "OX_AGENT_ID", w.ID)
	}

	if err := tmuxutil.SendKeys(w.TmuxSession, workerClaudeCmd(m, w, fresh, prev)); err != nil {
		return fmt.Errorf("relaunch worker claude: %w", err)
	}

	if err := UpdateRegistry(cfg.Home, m, func(reg *Registry) error {
		cur := reg.Workers[w.ID]
		if cur == nil {
			return fmt.Errorf("worker %q vanished from registry", w.ID)
		}
		cur.Status = WorkerRunning
		cur.FinishedAt = nil
		cur.SessionIDs = append(cur.SessionIDs, fresh)
		*w = *cur
		return nil
	}); err != nil {
		return err
	}

	msg := "Session restarted. Check git log and your earlier context, continue from where you left off. When done: commit, report_done, /exit."
	if extraContext != "" {
		msg += " Additional context: " + extraContext
	}
	go kickWorker(w.TmuxSession, msg)

	m.AppendEvent("agent_started", "system", map[string]any{"id": w.ID, "resumed_from": prev})
	return nil
}

// KillWorker terminates a worker's session and releases its locks.
func KillWorker(cfg *config.Config, m *mission.Mission, w *Worker, reason string) error {
	if tmuxutil.HasSession(w.TmuxSession) {
		tmuxutil.KillSession(w.TmuxSession)
	}
	if err := MarkWorkerFinished(cfg.Home, m, w.ID, WorkerKilled, ""); err != nil {
		return err
	}
	m.AppendEvent("agent_status", "system", map[string]any{"id": w.ID, "status": WorkerKilled, "reason": reason})
	return nil
}

// MarkWorkerFinished is the single terminal-transition path: status, finish
// time, lock release.
func MarkWorkerFinished(oxHome string, m *mission.Mission, workerID, status, summary string) error {
	return UpdateRegistry(oxHome, m, func(reg *Registry) error {
		w := reg.Workers[workerID]
		if w == nil {
			return fmt.Errorf("worker %q not found", workerID)
		}
		now := time.Now()
		w.Status = status
		w.FinishedAt = &now
		if summary != "" {
			w.Summary = summary
		}
		if len(w.Files) > 0 {
			filelock.NewManager(m.Dir()).Release(w.ID)
		}
		return nil
	})
}

// FindWorker locates a worker by ID across open missions.
func FindWorker(oxHome, workerID string) (*mission.Mission, *Worker, error) {
	missions, err := mission.List(oxHome)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range missions {
		if !m.Open() {
			continue
		}
		reg, err := LoadRegistry(m)
		if err != nil {
			continue
		}
		if w, ok := reg.Workers[workerID]; ok {
			return m, w, nil
		}
	}
	return nil, nil, fmt.Errorf("no worker %q in any open mission", workerID)
}

func unmetDeps(m *mission.Mission, w *Worker) []string {
	if len(w.DependsOn) == 0 {
		return nil
	}
	reg, err := LoadRegistry(m)
	if err != nil {
		return w.DependsOn
	}
	var unmet []string
	for _, dep := range w.DependsOn {
		d, ok := reg.Workers[dep]
		if !ok || d.Status != WorkerDone {
			unmet = append(unmet, dep)
		}
	}
	return unmet
}

func ensureWorktree(cfg *config.Config, w *Worker) error {
	if _, err := os.Stat(w.WorktreePath); err == nil {
		return nil
	}

	rc := cfg.Repos[w.Repo]
	repoPath := filepath.Join(cfg.Home, "repos", w.Repo)
	os.MkdirAll(filepath.Dir(w.WorktreePath), 0o755)

	baseBranch := rc.BaseBranch
	if baseBranch == "" {
		baseBranch = "origin/main"
	}
	if !strings.Contains(baseBranch, "/") {
		baseBranch = "origin/" + baseBranch
	}

	// Base-clone git operations are serialized per repo: concurrent worktree
	// adds/fetches from two missions race on .git locks otherwise.
	return withRepoLock(cfg.Home, w.Repo, func() error {
		if err := gitutil.Fetch(repoPath); err != nil {
			return fmt.Errorf("fetch %s: %w", w.Repo, err)
		}
		if err := gitutil.CreateWorktreeFromRef(repoPath, w.WorktreePath, w.BranchName, baseBranch); err != nil {
			return fmt.Errorf("worktree for %s: %w", w.ID, err)
		}
		for _, file := range rc.CopyFiles {
			copyPathHelper(filepath.Join(repoPath, file), filepath.Join(w.WorktreePath, file))
		}
		return nil
	})
}

func writeWorkerFiles(cfg *config.Config, m *mission.Mission, w *Worker) error {
	brief, err := os.ReadFile(workerFile(m, w.ID, "brief.md"))
	if err != nil {
		return fmt.Errorf("read brief: %w", err)
	}

	var agents strings.Builder
	fmt.Fprintf(&agents, "# Worker %s — mission %s\n\n", w.ID, m.ID)
	fmt.Fprintf(&agents, "Repo: %s · branch %s\n", w.Repo, w.BranchName)
	if len(w.Files) > 0 {
		agents.WriteString("\n## File ownership\nYou own ONLY these paths — do not modify anything else:\n")
		for _, f := range w.Files {
			fmt.Fprintf(&agents, "- `%s`\n", f)
		}
	}
	agents.WriteString("\n## Brief\n\n")
	agents.Write(brief)
	agents.WriteString("\n")
	if pk := PriorKnowledge(cfg, m, m.Goal+" "+string(brief[:min(len(brief), 200)]), 6); pk != "" {
		agents.WriteString("\n")
		agents.WriteString(pk)
	}
	if doc := RepoDoc(cfg.Home, w.Repo); doc != "" {
		agents.WriteString("\n## Repo knowledge (living doc — the distiller keeps it current)\n\n")
		agents.WriteString(doc)
		agents.WriteString("\n")
	}

	if err := os.WriteFile(filepath.Join(w.WorktreePath, "AGENTS.md"), []byte(agents.String()), 0o644); err != nil {
		return err
	}
	claudePath := filepath.Join(w.WorktreePath, "CLAUDE.md")
	os.Remove(claudePath)
	os.Symlink("AGENTS.md", claudePath)

	if _, err := WriteWorkerMCPConfig(m, w.ID); err != nil {
		return err
	}

	prompt := workerPrompt(cfg.Home, w)
	return os.WriteFile(workerFile(m, w.ID, "prompt.md"), []byte(prompt), 0o644)
}

// workerPrompt = worker-core + hygiene + persona body. Stable per worker so
// the session prompt cache holds.
func workerPrompt(oxHome string, w *Worker) string {
	var sb strings.Builder
	sb.WriteString(embedded("worker-core.md"))
	sb.WriteString("\n")
	sb.WriteString(embedded("hygiene.md"))

	if reg, err := personas.LoadRegistry(oxHome); err == nil {
		if p, ok := reg.Get(w.Persona); ok {
			sb.WriteString("\n---\n\n# Persona\n\n")
			sb.WriteString(p.Content)
		}
	}
	return sb.String()
}

func workerClaudeCmd(m *mission.Mission, w *Worker, sessionID, resumeID string) string {
	promptFile := workerFile(m, w.ID, "prompt.md")
	mcpFile := filepath.Join(m.Dir(), "workers", w.ID, "mcp.json")

	base := fmt.Sprintf("claude --dangerously-skip-permissions --model %s --append-system-prompt \"$(cat '%s')\" --mcp-config '%s' --strict-mcp-config",
		w.Model, promptFile, mcpFile)
	if w.MaxTurns > 0 {
		base += fmt.Sprintf(" --max-turns %d", w.MaxTurns)
	}

	fresh := fmt.Sprintf("%s --session-id %s", base, sessionID)
	if resumeID == "" {
		return fresh
	}
	return fmt.Sprintf("%s --resume %s || %s", base, resumeID, fresh)
}

func workerFile(m *mission.Mission, workerID, name string) string {
	dir := filepath.Join(m.Dir(), "workers", workerID)
	os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, name)
}

func kickWorker(session, msg string) {
	if !EnsureClaudeReady(session, 90*time.Second) {
		return
	}
	SendMessageEnsured(session, msg)
}

func personaModel(cfg *config.Config, persona, fallback string) string {
	if m, ok := cfg.Models.Personas[persona]; ok && m != "" {
		return m
	}
	return fallback
}

func withRepoLock(oxHome, repo string, fn func() error) error {
	return mission.WithFileLock(filepath.Join(oxHome, "repos", "."+repo+".lock"), fn)
}

func copyPathHelper(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		os.MkdirAll(dst, info.Mode())
		for _, e := range entries {
			copyPathHelper(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()))
		}
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	return os.WriteFile(dst, data, info.Mode())
}
