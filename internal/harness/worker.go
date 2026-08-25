package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	Engine       string     `json:"engine,omitempty"` // "" / "claude" (default) | "opencode"
	Repo         string     `json:"repo"`
	Status       string     `json:"status"`
	TmuxSession  string     `json:"tmux_session"`
	TmuxPane     string     `json:"tmux_pane,omitempty"` // set in pane layout: agent is a pane (%N) in the mission session, not its own session
	WorktreePath string     `json:"worktree_path"`
	BranchName   string     `json:"branch_name"`
	SessionIDs   []string   `json:"session_ids"`
	SharedCwd    bool       `json:"shared_cwd,omitempty"` // runs in a dir it doesn't own: no branch, never cleaned up
	Files        []string   `json:"files,omitempty"`
	DependsOn    []string   `json:"depends_on,omitempty"`
	MaxTurns     int        `json:"max_turns,omitempty"`
	MaxBudgetUSD float64    `json:"max_budget_usd,omitempty"`
	SpendUSD     float64    `json:"spend_usd"`
	SpawnedAt    time.Time  `json:"spawned_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Summary      string     `json:"summary,omitempty"`
}

// UsesOpencode reports whether this worker runs on opencode (any model)
// rather than claude (the default).
func (w *Worker) UsesOpencode() bool { return w.Engine == "opencode" }

func (w *Worker) Finished() bool {
	return w.Status == WorkerDone || w.Status == WorkerFailed || w.Status == WorkerKilled
}

// Paned reports whether this worker lives as a pane in the mission session
// (pane layout) rather than its own tmux session. The pane id's presence is
// the mode flag — workers created before pane layout have none and stay
// session-based, so the two models coexist safely.
func (w *Worker) Paned() bool { return w.TmuxPane != "" }

// Target is the tmux address for send-keys / capture-pane / etc.
func (w *Worker) Target() string {
	if w.Paned() {
		return w.TmuxPane
	}
	return w.TmuxSession
}

// Alive reports whether the worker's tmux surface still exists.
func (w *Worker) Alive() bool {
	if w.Paned() {
		return tmuxutil.PaneAlive(w.TmuxPane)
	}
	return tmuxutil.HasSession(w.TmuxSession)
}

// Teardown closes the worker's tmux surface: its pane, or its whole session.
func (w *Worker) Teardown() {
	if w.Paned() {
		tmuxutil.KillPane(w.TmuxPane)
		return
	}
	if tmuxutil.HasSession(w.TmuxSession) {
		tmuxutil.KillSession(w.TmuxSession)
	}
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
	Engine       string
	Cwd          string // existing directory to run in (e.g. a review worktree); skips worktree/branch creation
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
	if m.SpendFrozen && cfg.Budgets.Enforce {
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
		ID: in.ID, MissionID: m.ID, Persona: in.Persona, Model: in.Model, Engine: in.Engine, Repo: in.Repo,
		Status: WorkerPending, Files: in.Files, DependsOn: in.DependsOn,
		MaxTurns: in.MaxTurns, MaxBudgetUSD: in.MaxBudgetUSD,
		TmuxSession:  fmt.Sprintf("ox-%s-%s", m.ID, in.ID),
		WorktreePath: filepath.Join(cfg.Home, "worktrees", in.Repo, fmt.Sprintf("%s-%s", m.ID, in.ID)),
		BranchName:   fmt.Sprintf("ox/%s-%s", m.ID, in.ID),
		SpawnedAt:    time.Now(),
	}
	if in.Cwd != "" {
		if _, err := os.Stat(in.Cwd); err != nil {
			return nil, fmt.Errorf("cwd %s does not exist", in.Cwd)
		}
		w.WorktreePath = in.Cwd
		w.BranchName = ""
		w.SharedCwd = true
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
	if !w.SharedCwd {
		if err := ensureWorktree(cfg, w); err != nil {
			return err
		}
	}
	if err := writeWorkerFiles(cfg, m, w); err != nil {
		return err
	}

	sessionID := uuid.NewString()

	// Pane layout: the worker is a tiled pane in the mission session, so it
	// sits on one screen beside the orchestrator. Session layout (default):
	// its own session. The chosen surface's target then drives every
	// downstream tmux call uniformly via w.Target().
	pane := ""
	if cfg.PaneLayout() && tmuxutil.HasSession(m.TmuxSession()) {
		p, err := tmuxutil.EnsureAgentPane(m.TmuxSession(), "agents", w.WorktreePath)
		if err != nil {
			return fmt.Errorf("worker pane: %w", err)
		}
		pane = p
		tmuxutil.SetPaneTitle(pane, w.ID)
	} else {
		if tmuxutil.HasSession(w.TmuxSession) {
			tmuxutil.KillSession(w.TmuxSession)
		}
		if err := tmuxutil.NewSession(w.TmuxSession, w.WorktreePath); err != nil {
			return fmt.Errorf("worker tmux session: %w", err)
		}
		tmuxutil.RenameWindow(w.TmuxSession, w.ID)
		tmuxutil.SetEnv(w.TmuxSession, "OX_MISSION_ID", m.ID)
		tmuxutil.SetEnv(w.TmuxSession, "OX_AGENT_ID", w.ID)
	}
	w.TmuxPane = pane
	target := w.Target()

	if err := tmuxutil.SendKeys(target, workerClaudeCmd(m, w, sessionID, "")); err != nil {
		return fmt.Errorf("launch worker claude: %w", err)
	}

	if err := UpdateRegistry(cfg.Home, m, func(reg *Registry) error {
		cur := reg.Workers[w.ID]
		if cur == nil {
			return fmt.Errorf("worker %q vanished from registry", w.ID)
		}
		cur.Status = WorkerRunning
		cur.TmuxPane = pane
		cur.SessionIDs = append(cur.SessionIDs, sessionID)
		cur.FinishedAt = nil
		*w = *cur
		return nil
	}); err != nil {
		return err
	}

	briefRef := "AGENTS.md"
	doneVerb := "commit, call report_done"
	if w.SharedCwd {
		briefRef = workerFile(m, w.ID, "AGENTS.md")
		doneVerb = "call report_done"
	}
	kick := kickWorker
	if w.UsesOpencode() {
		kick = kickOpencode
	}
	go kick(target,
		fmt.Sprintf("You are worker '%s'. Read %s for your brief, then BEGIN IMMEDIATELY. When completely done: %s, then /exit. Do not ask for confirmation.", w.ID, briefRef, doneVerb))

	m.AppendEvent("agent_started", "system", map[string]any{"id": w.ID, "session": sessionID})
	return nil
}

// RespawnWorker restarts a dead worker RESUME-FIRST: the previous claude
// conversation is restored in the surviving worktree; a fresh session with a
// kick message is only the fallback when no transcript exists. A lingering
// tmux session with a dead claude (worker ran /exit, shell survived) is
// reused rather than treated as "already running".
func RespawnWorker(cfg *config.Config, m *mission.Mission, w *Worker, extraContext string) error {
	surfaceAlive := w.Alive()
	if surfaceAlive && ClaudeAlive(w.Target()) {
		return fmt.Errorf("worker %q is already running", w.ID)
	}
	if _, err := os.Stat(w.WorktreePath); err != nil {
		return fmt.Errorf("worktree gone (%s) — spawn a fresh worker instead", w.WorktreePath)
	}

	prev := w.LastSessionID()
	fresh := uuid.NewString()

	// A paned worker respawns as a fresh pane in the mission session; a
	// session worker respawns as (or reuses) its session. A reused live
	// surface keeps its target.
	if !surfaceAlive {
		if w.Paned() {
			p, err := tmuxutil.EnsureAgentPane(m.TmuxSession(), "agents", w.WorktreePath)
			if err != nil {
				return fmt.Errorf("worker pane: %w", err)
			}
			w.TmuxPane = p
			tmuxutil.SetPaneTitle(p, w.ID)
		} else {
			if err := tmuxutil.NewSession(w.TmuxSession, w.WorktreePath); err != nil {
				return fmt.Errorf("worker tmux session: %w", err)
			}
			tmuxutil.RenameWindow(w.TmuxSession, w.ID)
			tmuxutil.SetEnv(w.TmuxSession, "OX_MISSION_ID", m.ID)
			tmuxutil.SetEnv(w.TmuxSession, "OX_AGENT_ID", w.ID)
		}
	}
	target := w.Target()

	if err := tmuxutil.SendKeys(target, workerClaudeCmd(m, w, fresh, prev)); err != nil {
		return fmt.Errorf("relaunch worker claude: %w", err)
	}

	newPane := w.TmuxPane
	if err := UpdateRegistry(cfg.Home, m, func(reg *Registry) error {
		cur := reg.Workers[w.ID]
		if cur == nil {
			return fmt.Errorf("worker %q vanished from registry", w.ID)
		}
		cur.Status = WorkerRunning
		cur.TmuxPane = newPane
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
	kick := kickWorker
	if w.UsesOpencode() {
		kick = kickOpencode
	}
	go kick(target, msg)

	m.AppendEvent("agent_started", "system", map[string]any{"id": w.ID, "resumed_from": prev})
	return nil
}

// KillWorker terminates a worker's session and releases its locks.
func KillWorker(cfg *config.Config, m *mission.Mission, w *Worker, reason string) error {
	w.Teardown()
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
	if err := withRepoLock(cfg.Home, w.Repo, func() error {
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
	}); err != nil {
		return err
	}

	// Outside the repo lock: npm install and friends are slow and only touch
	// the new worktree.
	return runPostSetup(rc, w.WorktreePath)
}

// runPostSetup runs the repo's configured setup command in a fresh worktree.
// Output goes to setup.log in the worktree — stdout is off-limits here, the
// MCP server owns it for JSON-RPC. Failure is surfaced but non-fatal, like
// the legacy paths.
func runPostSetup(rc *config.RepoConfig, worktree string) error {
	if rc == nil || rc.PostSetup == "" {
		return nil
	}
	logF, err := os.Create(filepath.Join(worktree, ".ox-setup.log"))
	if err != nil {
		return nil
	}
	defer logF.Close()

	cmd := exec.Command("sh", "-c", rc.PostSetup)
	cmd.Dir = worktree
	cmd.Stdout = logF
	cmd.Stderr = logF
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: post-setup (%s) failed in %s: %v — see .ox-setup.log\n", rc.PostSetup, worktree, err)
	}
	return nil
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

	// Shared-cwd workers (several may run in the same directory, e.g. a
	// review worktree) keep their AGENTS.md in the mission's worker dir —
	// writing into the shared dir would collide and litter it.
	agentsPath := filepath.Join(w.WorktreePath, "AGENTS.md")
	if w.SharedCwd {
		agentsPath = workerFile(m, w.ID, "AGENTS.md")
	}
	if err := os.WriteFile(agentsPath, []byte(agents.String()), 0o644); err != nil {
		return err
	}
	if !w.SharedCwd {
		claudePath := filepath.Join(w.WorktreePath, "CLAUDE.md")
		os.Remove(claudePath)
		os.Symlink("AGENTS.md", claudePath)
	}

	if w.UsesOpencode() {
		if err := writeOpencodeWorkerConfig(cfg, m, w, agentsPath); err != nil {
			return err
		}
	}

	if _, err := WriteWorkerMCPConfig(m, w.ID); err != nil {
		return err
	}

	prompt := workerPrompt(cfg.Home, w)
	return os.WriteFile(workerFile(m, w.ID, "prompt.md"), []byte(prompt), 0o644)
}

// writeOpencodeWorkerConfig wires an opencode worker: an opencode.json in the
// worktree carrying the model + the ox MCP server (so report_done etc. work),
// and the worker doctrine folded into AGENTS.md — opencode reads AGENTS.md
// natively and has no --append-system-prompt.
func writeOpencodeWorkerConfig(cfg *config.Config, m *mission.Mission, w *Worker, agentsPath string) error {
	oxBin, err := os.Executable()
	if err != nil {
		oxBin = "ox"
	}
	conf := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"model":   w.Model,
		"mcp": map[string]any{
			"ox": map[string]any{
				"type":    "local",
				"enabled": true,
				"command": []string{oxBin, "mcp", "--mission", m.ID, "--role", "worker", "--agent", w.ID},
			},
		},
	}
	data, _ := json.MarshalIndent(conf, "", "  ")
	if err := os.WriteFile(filepath.Join(w.WorktreePath, "opencode.json"), data, 0o644); err != nil {
		return fmt.Errorf("write opencode.json: %w", err)
	}
	f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n\n---\n\n" + workerPrompt(cfg.Home, w))
	return err
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
	if w.UsesOpencode() {
		// Model + ox MCP + brief all come from opencode.json / AGENTS.md in
		// the worktree (written by writeWorkerFiles); opencode reads them
		// natively. No resume in v1.
		return "opencode"
	}
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

// kickOpencode nudges an opencode worker. opencode's TUI has no claude-style
// readiness marker, so this is delay-based best-effort: wait for the TUI to
// paint, type the message, submit.
func kickOpencode(target, msg string) {
	time.Sleep(6 * time.Second)
	tmuxutil.SendKeys(target, msg)
	time.Sleep(500 * time.Millisecond)
	tmuxutil.SendKeysRaw(target, "Enter")
}

func personaModel(cfg *config.Config, persona, fallback string) string {
	// Explicit config override wins; then the persona's own declared tier
	// (so a cheap persona like `fixer` resolves to its model without needing
	// per-install config); then the caller's fallback.
	if m, ok := cfg.Models.Personas[persona]; ok && m != "" {
		return m
	}
	if reg, err := personas.LoadRegistry(cfg.Home); err == nil {
		if p, ok := reg.Get(persona); ok && p.DefaultModel != "" {
			return p.DefaultModel
		}
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
