// Package watcher is the mission's event loop: it reconciles worker/job
// state, starts dependency-gated workers, and feeds the orchestrator digests
// through guarded tmux injection. It runs as a visible foreground process in
// the mission session's "watch" window and recomputes everything from disk,
// so killing it at any moment loses nothing.
package watcher

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/costs"
	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

const (
	tick          = 5 * time.Second
	slowEvery     = 6 // heartbeat every 6th tick
	batchInterval = 3 * time.Minute
	idleAfter     = 10 * time.Minute
)

type state struct {
	HeartbeatAt     time.Time        `json:"heartbeat_at"`
	EventCursor     int64            `json:"event_cursor"`
	LastInjectionAt time.Time        `json:"last_injection_at"`
	PanelsNotified  map[string]bool  `json:"panels_notified,omitempty"`
	IdleNotified    map[string]bool  `json:"idle_notified,omitempty"`
	Offsets         map[string]int64 `json:"transcript_offsets,omitempty"` // session id → byte offset
	WorkerWarned    map[string]bool  `json:"worker_warned,omitempty"`
	WorkerGraceAt   map[string]int64 `json:"worker_grace_at,omitempty"` // unix — wrap-up notice sent
	ContextWarned   bool             `json:"context_warned,omitempty"`
}

type Watcher struct {
	cfg       *config.Config
	missionID string
	st        state

	paneHash   map[string]uint64
	paneChange map[string]time.Time
	deadMisses map[string]int
}

func New(cfg *config.Config, missionID string) *Watcher {
	return &Watcher{
		cfg: cfg, missionID: missionID,
		paneHash: map[string]uint64{}, paneChange: map[string]time.Time{},
		deadMisses: map[string]int{},
	}
}

// Run loops until the mission closes or the process is killed.
func (w *Watcher) Run() error {
	m, err := mission.Open(w.cfg.Home, w.missionID)
	if err != nil {
		return err
	}
	w.loadState(m)
	fmt.Printf("watcher: mission %s (%s) — tick %s\n", m.ID, m.Goal, tick)
	m.AppendEvent("watcher_started", "system", nil)

	n := 0
	for {
		m, err = mission.Open(w.cfg.Home, w.missionID)
		if err != nil {
			return err
		}
		if !m.Open() {
			fmt.Println("watcher: mission closed, exiting")
			return nil
		}

		w.reconcileWorkers(m)
		w.harvestJobs(m)
		w.startUnblocked(m)
		w.checkIdle(m)
		w.pumpDigests(m)

		n++
		if n%slowEvery == 0 {
			w.trackCosts(m)
			w.enforceBudgets(m)
			w.st.HeartbeatAt = time.Now()
			w.saveState(m)
		}
		time.Sleep(tick)
	}
}

// trackCosts tails every interactive transcript (orchestrator + workers) and
// prices the new usage into the ledger and mission rollup.
func (w *Watcher) trackCosts(m *mission.Mission) {
	if w.st.Offsets == nil {
		w.st.Offsets = map[string]int64{}
	}

	total := 0.0
	// Orchestrator: transcript lives under the mission dir cwd.
	if sid := m.Orchestrator.SessionID; sid != "" {
		if cost, ctxTokens := w.tailSession(m.Dir(), sid, m.Orchestrator.Model); cost > 0 {
			total += cost
			appendLedger(m, "orchestrator", "orchestrator", m.Orchestrator.Model, cost)
			w.adviseCompaction(m, ctxTokens)
		} else if ctxTokens > 0 {
			w.adviseCompaction(m, ctxTokens)
		}
	}

	reg, err := harness.LoadRegistry(m)
	if err == nil {
		for _, worker := range reg.Workers {
			workerCost := 0.0
			for _, sid := range worker.SessionIDs {
				cost, _ := w.tailSession(worker.WorktreePath, sid, worker.Model)
				workerCost += cost
			}
			if workerCost > 0 {
				total += workerCost
				appendLedger(m, worker.ID, "session", worker.Model, workerCost)
				id := worker.ID
				harness.UpdateRegistry(w.cfg.Home, m, func(reg *harness.Registry) error {
					if cur := reg.Workers[id]; cur != nil {
						cur.SpendUSD += workerCost
					}
					return nil
				})
			}
		}
	}

	if total > 0 {
		mission.Update(w.cfg.Home, m.ID, func(mm *mission.Mission) error {
			mm.SpentUSD += total
			return nil
		})
	}
}

func (w *Watcher) tailSession(cwd, sessionID, model string) (float64, int64) {
	path := costs.TranscriptPath(cwd, sessionID)
	delta, ctxTokens, newOffset, err := costs.Tail(path, w.st.Offsets[sessionID])
	if err != nil {
		return 0, 0
	}
	w.st.Offsets[sessionID] = newOffset
	return delta.CostUSD(model), ctxTokens
}

// adviseCompaction nudges the orchestrator once when its context grows past
// the comfortable zone.
func (w *Watcher) adviseCompaction(m *mission.Mission, ctxTokens int64) {
	if ctxTokens < 140_000 || w.st.ContextWarned {
		return
	}
	w.st.ContextWarned = true
	m.AppendEvent("budget_warning", "system", map[string]any{
		"detail": fmt.Sprintf("orchestrator context ~%dk tokens — checkpoint state to disk and /compact at the next phase boundary", ctxTokens/1000),
	})
}

// enforceBudgets warns at 80%% of a worker budget, then asks the worker to
// wrap up at 100%%, then kills after a grace period. Mission-level: warn at
// 70%%, freeze spawning at 100%%.
func (w *Watcher) enforceBudgets(m *mission.Mission) {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return
	}
	if w.st.WorkerWarned == nil {
		w.st.WorkerWarned = map[string]bool{}
	}
	if w.st.WorkerGraceAt == nil {
		w.st.WorkerGraceAt = map[string]int64{}
	}

	for _, worker := range reg.Workers {
		if worker.Finished() || worker.MaxBudgetUSD <= 0 {
			continue
		}
		frac := worker.SpendUSD / worker.MaxBudgetUSD
		switch {
		case frac >= 1.0:
			if graceAt, ok := w.st.WorkerGraceAt[worker.ID]; !ok {
				w.st.WorkerGraceAt[worker.ID] = time.Now().Unix()
				if injectSafe(worker.TmuxSession) {
					tmuxutil.SendKeys(worker.TmuxSession,
						"Budget limit reached — commit what you have now, call report_done with the current state, and /exit.")
				}
				m.AppendEvent("budget_warning", "system", map[string]any{
					"detail": fmt.Sprintf("%s hit its budget ($%.2f) — asked to wrap up", worker.ID, worker.MaxBudgetUSD),
				})
			} else if time.Since(time.Unix(graceAt, 0)) > 3*time.Minute {
				harness.KillWorker(w.cfg, m, worker, "budget")
				m.AppendEvent("budget_exceeded", "system", map[string]any{
					"detail": fmt.Sprintf("%s killed after budget grace ($%.2f spent)", worker.ID, worker.SpendUSD),
				})
				delete(w.st.WorkerGraceAt, worker.ID)
			}
		case frac >= 0.8 && !w.st.WorkerWarned[worker.ID]:
			w.st.WorkerWarned[worker.ID] = true
			m.AppendEvent("budget_warning", "system", map[string]any{
				"detail": fmt.Sprintf("%s at %.0f%% of its $%.2f budget", worker.ID, frac*100, worker.MaxBudgetUSD),
			})
		}
	}

	if m.Budgets.MissionUSD <= 0 {
		return
	}
	frac := m.SpentUSD / m.Budgets.MissionUSD
	switch {
	case frac >= 1.0 && !m.SpendFrozen:
		mission.Update(w.cfg.Home, m.ID, func(mm *mission.Mission) error {
			mm.SpendFrozen = true
			return nil
		})
		m.AppendEvent("budget_exceeded", "system", map[string]any{
			"detail": fmt.Sprintf("mission spent $%.2f of $%.2f — spawning frozen until the budget is raised", m.SpentUSD, m.Budgets.MissionUSD),
		})
	case frac >= 0.7 && !m.Budgets.Warned70:
		mission.Update(w.cfg.Home, m.ID, func(mm *mission.Mission) error {
			mm.Budgets.Warned70 = true
			return nil
		})
		m.AppendEvent("budget_warning", "system", map[string]any{
			"detail": fmt.Sprintf("mission at %.0f%% of $%.2f budget", frac*100, m.Budgets.MissionUSD),
		})
	}
}

// appendLedger writes an estimated-cost entry for interactive sessions.
func appendLedger(m *mission.Mission, actor, kind, model string, cost float64) {
	entry, _ := json.Marshal(map[string]any{
		"ts": time.Now().Format(time.RFC3339), "actor": actor, "kind": kind,
		"model": model, "cost_usd": cost, "source": "estimate",
	})
	f, err := os.OpenFile(filepath.Join(m.Dir(), "ledger.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		f.Write(append(entry, '\n'))
		f.Close()
	}
}

// reconcileWorkers detects abnormal exits: a worker still marked running
// whose session or claude is gone — and never reported done — is interrupted,
// not silently done.
func (w *Watcher) reconcileWorkers(m *mission.Mission) {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return
	}
	for _, worker := range reg.Workers {
		if worker.Status != harness.WorkerRunning && worker.Status != harness.WorkerBlocked {
			continue
		}
		alive := tmuxutil.HasSession(worker.TmuxSession) && harness.ClaudeAlive(worker.TmuxSession)
		if alive {
			w.deadMisses[worker.ID] = 0
			continue
		}
		// Grace: claude may be between launch and first paint.
		if time.Since(worker.SpawnedAt) < 2*time.Minute {
			continue
		}
		// Two consecutive dead ticks before flipping: transient tmux server
		// hiccups and mid-render captures read as dead for a single tick.
		w.deadMisses[worker.ID]++
		if w.deadMisses[worker.ID] < 2 {
			continue
		}
		w.deadMisses[worker.ID] = 0
		id := worker.ID
		harness.UpdateRegistry(w.cfg.Home, m, func(reg *harness.Registry) error {
			if cur := reg.Workers[id]; cur != nil &&
				(cur.Status == harness.WorkerRunning || cur.Status == harness.WorkerBlocked) {
				cur.Status = harness.WorkerInterrupted
			}
			return nil
		})
		fmt.Printf("watcher: %s interrupted (session/claude gone without report_done)\n", id)
		m.AppendEvent("agent_interrupted", "system", map[string]any{"id": id})
	}
}

// harvestJobs finalizes finished jobs, auto-applies the retry/escalation
// ladder to failures, and emits panel_done when a whole panel lands.
func (w *Watcher) harvestJobs(m *mission.Mission) {
	finished := job.HarvestAll(w.cfg, m)
	for _, j := range finished {
		if j.Status == job.StatusFailed {
			if retried, _ := job.RetryOrEscalate(w.cfg, m, j); retried != nil {
				fmt.Printf("watcher: job %s failed → attempt %d on %s\n", j.ID, retried.Attempts, retried.Model)
			}
		}
	}

	idx, err := job.LoadIndex(m)
	if err != nil {
		return
	}
	byPanel := map[string][]*job.Job{}
	for _, j := range idx.Jobs {
		if j.PanelID != "" {
			byPanel[j.PanelID] = append(byPanel[j.PanelID], j)
		}
	}
	for panel, jobs := range byPanel {
		if w.st.PanelsNotified[panel] {
			continue
		}
		allDone := true
		for _, j := range jobs {
			if j.Status == job.StatusRunning {
				allDone = false
				break
			}
		}
		if allDone {
			if w.st.PanelsNotified == nil {
				w.st.PanelsNotified = map[string]bool{}
			}
			w.st.PanelsNotified[panel] = true
			m.AppendEvent("panel_done", "system", map[string]any{"panel": panel, "jobs": len(jobs)})
		}
	}
}

// startUnblocked spawns pending workers whose dependencies all finished —
// the half of dependency gating the old pipeline never implemented.
func (w *Watcher) startUnblocked(m *mission.Mission) {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return
	}
	for _, worker := range reg.Workers {
		if worker.Status != harness.WorkerPending {
			continue
		}
		ready := true
		failedDep := ""
		for _, dep := range worker.DependsOn {
			d, ok := reg.Workers[dep]
			if !ok {
				ready = false
				break
			}
			switch d.Status {
			case harness.WorkerDone:
			case harness.WorkerFailed, harness.WorkerKilled:
				failedDep = dep
				ready = false
			default:
				ready = false
			}
		}
		if failedDep != "" {
			m.AppendEvent("agent_blocker", "system", map[string]any{
				"id": worker.ID, "question": fmt.Sprintf("dependency %s ended %s — respawn it, re-plan, or force-start %s?", failedDep, reg.Workers[failedDep].Status, worker.ID),
			})
			continue
		}
		if !ready {
			continue
		}
		fmt.Printf("watcher: dependencies met — starting %s\n", worker.ID)
		if err := harness.StartWorker(w.cfg, m, worker); err != nil {
			fmt.Printf("watcher: start %s failed: %v\n", worker.ID, err)
			continue
		}
		m.AppendEvent("deps_unblocked", "system", map[string]any{"id": worker.ID})
	}
}

// checkIdle flags workers whose pane hasn't changed in a while at an input
// prompt — usually a worker waiting on something nobody saw.
func (w *Watcher) checkIdle(m *mission.Mission) {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return
	}
	for _, worker := range reg.Workers {
		if worker.Status != harness.WorkerRunning {
			continue
		}
		if !tmuxutil.HasSession(worker.TmuxSession) {
			continue
		}
		pane, err := tmuxutil.CapturePane(worker.TmuxSession, 30)
		if err != nil {
			continue
		}
		h := fnv.New64a()
		h.Write([]byte(pane))
		sum := h.Sum64()

		if w.paneHash[worker.ID] != sum {
			w.paneHash[worker.ID] = sum
			w.paneChange[worker.ID] = time.Now()
			delete(w.st.IdleNotified, worker.ID)
			continue
		}
		if time.Since(w.paneChange[worker.ID]) > idleAfter && !w.st.IdleNotified[worker.ID] &&
			strings.Contains(pane, "bypass permissions") {
			if w.st.IdleNotified == nil {
				w.st.IdleNotified = map[string]bool{}
			}
			w.st.IdleNotified[worker.ID] = true
			m.AppendEvent("agent_idle", "system", map[string]any{
				"id": worker.ID, "idle_min": int(time.Since(w.paneChange[worker.ID]).Minutes()),
			})
		}
	}
}

// event classification for the digest pump.
var priorityEvents = map[string]bool{
	"agent_done":        true,
	"agent_blocker":     true,
	"agent_interrupted": true,
	"panel_done":        true,
	"job_failed":        true,
	"budget_exceeded":   true,
	"merge_failed":      true,
}

var batchedEvents = map[string]bool{
	"job_done":       true,
	"deps_unblocked": true,
	"budget_warning": true,
	"agent_idle":     true,
}

// pumpDigests reads new events past the cursor and injects them into the
// orchestrator pane: priority events as a real message (needs a turn),
// batched ones as /btw context at most every batchInterval. Single-injector
// rule: nothing else ever send-keys into the orc window.
func (w *Watcher) pumpDigests(m *mission.Mission) {
	events, err := m.EventsSince(w.st.EventCursor)
	if err != nil || len(events) == 0 {
		return
	}

	var priority, batched []string
	maxN := w.st.EventCursor
	for _, ev := range events {
		if ev.N > maxN {
			maxN = ev.N
		}
		switch {
		case priorityEvents[ev.Type]:
			priority = append(priority, eventLine(ev))
		case batchedEvents[ev.Type]:
			batched = append(batched, eventLine(ev))
		}
	}

	if len(priority) == 0 && (len(batched) == 0 || time.Since(w.st.LastInjectionAt) < batchInterval) {
		// Nothing urgent; leave batched items for a later flush by NOT
		// advancing the cursor past them only if we have none to send now.
		if len(batched) == 0 {
			w.st.EventCursor = maxN
			w.saveState(m)
		}
		return
	}

	target := m.TmuxSession() + ":orc"
	if !injectSafe(target) {
		fmt.Println("watcher: orchestrator busy — digest deferred")
		return
	}

	var msg string
	all := append(priority, batched...)
	line := fmt.Sprintf("[ox %s] %s", m.ID, strings.Join(all, " · "))
	if len(priority) > 0 {
		msg = line
	} else {
		msg = "/btw " + line
	}

	if err := harness.SendMessageEnsured(target, msg); err != nil {
		fmt.Printf("watcher: inject failed: %v\n", err)
		return
	}
	fmt.Printf("watcher: injected %d event(s)\n", len(all))
	w.st.EventCursor = maxN
	w.st.LastInjectionAt = time.Now()
	w.saveState(m)
	m.AppendEvent("digest_injected", "system", map[string]any{"events": len(all)})
	// The digest event itself must not re-trigger.
	w.st.EventCursor++
	w.saveState(m)
}

// injectSafe: claude prompt is up, nothing streaming, input line empty (the
// user is not mid-typing).
func injectSafe(target string) bool {
	out, err := tmuxutil.CapturePane(target, 15)
	if err != nil {
		return false
	}
	if strings.Contains(out, "esc to interrupt") {
		return false
	}
	if !strings.Contains(out, "bypass permissions") && !strings.Contains(out, "shift+tab to cycle") {
		return false
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "─") || strings.HasPrefix(line, "\\") ||
			strings.Contains(line, "bypass permissions") || strings.Contains(line, "for agents") {
			continue
		}
		if strings.HasPrefix(line, "❯") {
			return strings.TrimSpace(strings.TrimPrefix(line, "❯")) == ""
		}
		// Last meaningful line isn't the prompt — claude is mid-output.
		return false
	}
	return false
}

func eventLine(ev mission.Event) string {
	id, _ := ev.Data["id"].(string)
	switch ev.Type {
	case "agent_done":
		return fmt.Sprintf("%s DONE → workers/%s/output.md", id, id)
	case "agent_blocker":
		q, _ := ev.Data["question"].(string)
		return fmt.Sprintf("%s BLOCKED: %q", id, q)
	case "agent_interrupted":
		return fmt.Sprintf("%s INTERRUPTED (worktree intact — respawn_agent to resume)", id)
	case "agent_idle":
		mins, _ := ev.Data["idle_min"].(float64)
		return fmt.Sprintf("%s idle %dm", id, int(mins))
	case "panel_done":
		p, _ := ev.Data["panel"].(string)
		return fmt.Sprintf("panel %s finished → job_result", p)
	case "job_failed":
		e, _ := ev.Data["error"].(string)
		return fmt.Sprintf("job %s FAILED: %.60s", id, e)
	case "job_done":
		return fmt.Sprintf("job %s done", id)
	case "deps_unblocked":
		return fmt.Sprintf("%s auto-started (deps met)", id)
	case "budget_warning":
		return fmt.Sprintf("budget warning: %v", ev.Data["detail"])
	case "budget_exceeded":
		return fmt.Sprintf("BUDGET EXCEEDED: %v", ev.Data["detail"])
	case "merge_failed":
		return fmt.Sprintf("merge FAILED: %v", ev.Data["detail"])
	}
	return ev.Type
}

func (w *Watcher) statePath(m *mission.Mission) string {
	return filepath.Join(m.Dir(), "watcher-state.json")
}

func (w *Watcher) loadState(m *mission.Mission) {
	data, err := os.ReadFile(w.statePath(m))
	if err == nil {
		json.Unmarshal(data, &w.st)
	}
}

func (w *Watcher) saveState(m *mission.Mission) {
	data, _ := json.Marshal(w.st)
	os.WriteFile(w.statePath(m), data, 0o644)
}

// EnsureRunning respawns the watcher window when its heartbeat is stale.
// Called from ox go and mission_status.
func EnsureRunning(cfg *config.Config, m *mission.Mission) {
	session := m.TmuxSession()
	if !tmuxutil.HasSession(session) {
		return
	}

	stale := true
	if data, err := os.ReadFile(filepath.Join(m.Dir(), "watcher-state.json")); err == nil {
		var st state
		if json.Unmarshal(data, &st) == nil && time.Since(st.HeartbeatAt) < 90*time.Second {
			stale = false
		}
	}

	cmd := fmt.Sprintf("'%s' watch %s", oxBinary(), m.ID)
	if !tmuxutil.HasWindow(session, "watch") {
		tmuxutil.NewWindow(session, "watch", m.Dir(), cmd)
		return
	}
	if stale {
		tmuxutil.RespawnWindow(session+":watch", cmd)
	}
}

func oxBinary() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "ox"
}
