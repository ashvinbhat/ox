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
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/cmux"
	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/costs"
	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

const (
	tick       = 5 * time.Second
	slowEvery  = 6 // heartbeat every 6th tick
	idleAfter  = 10 * time.Minute
	prInterval = 90 * time.Second
	reapGrace  = 30 * time.Minute
	// compactAdviseTokens is when the orchestrator's context is large enough
	// to suggest compacting — ~80% of a 1M-token window. Advisory only.
	compactAdviseTokens = 800_000
)

type state struct {
	HeartbeatAt     time.Time         `json:"heartbeat_at"`
	EventCursor     int64             `json:"event_cursor"`
	LastInjectionAt time.Time         `json:"last_injection_at"`
	PanelsNotified  map[string]bool   `json:"panels_notified,omitempty"`
	IdleNotified    map[string]bool   `json:"idle_notified,omitempty"`
	Offsets         map[string]int64  `json:"transcript_offsets,omitempty"` // session id → byte offset
	WorkerWarned    map[string]bool   `json:"worker_warned,omitempty"`
	WorkerGraceAt   map[string]int64  `json:"worker_grace_at,omitempty"` // unix — wrap-up notice sent
	ContextWarned   bool              `json:"context_warned,omitempty"`
	PRStates        map[string]string `json:"pr_states,omitempty"` // url → last seen state signature
	LastPRPollAt    time.Time         `json:"last_pr_poll_at,omitempty"`
	OrcLastUserAt   time.Time         `json:"orc_last_user_at,omitempty"`
}

type Watcher struct {
	cfg       *config.Config
	missionID string
	st        state

	paneHash   map[string]uint64
	paneChange map[string]time.Time
	deadMisses map[string]int
	holdLogged int64
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
			cmux.CloseMission(m)
			return nil
		}

		w.reconcileWorkers(m)
		w.harvestJobs(m)
		w.startUnblocked(m)
		w.checkIdle(m)
		w.pumpDigests(m)

		n++
		if n%slowEvery == 0 {
			w.reapIdleWorkers(m)
			cmux.SyncMission(w.cfg, m)
			w.trackCosts(m)
			if w.cfg.Budgets.Enforce {
				w.enforceBudgets(m)
			}
			w.st.HeartbeatAt = time.Now()
			w.saveState(m)
		}
		// Wall-clock gated, not tick-counted: a watcher restart resets n but
		// not the clock, so PR polling stays on cadence across restarts.
		if time.Since(w.st.LastPRPollAt) >= prInterval {
			w.trackPRs(m)
			w.st.LastPRPollAt = time.Now()
			w.saveState(m)
		}
		time.Sleep(tick)
	}
}

// trackPRs polls linked PRs while the mission is in a shipped state and
// surfaces review activity: the orchestrator stays responsible for the PR
// until it merges. Comment count spans BOTH the conversation tab and inline
// review comments — the latter (code-line feedback) is what reviewers
// actually leave and gh pr view omits it.
func (w *Watcher) trackPRs(m *mission.Mission) {
	if m.Phase != "shipping" && m.Phase != "reviewing" {
		return
	}
	if w.st.PRStates == nil {
		w.st.PRStates = map[string]string{}
	}
	for _, pr := range m.PRs {
		out, err := exec.Command("gh", "pr", "view", pr.URL, "--json", "state,reviewDecision,comments,reviews").Output()
		if err != nil {
			continue
		}
		var info struct {
			State          string `json:"state"`
			ReviewDecision string `json:"reviewDecision"`
			Comments       []any  `json:"comments"`
			Reviews        []any  `json:"reviews"`
		}
		if json.Unmarshal(out, &info) != nil {
			continue
		}
		comments := len(info.Comments) + prReviewCommentCount(pr.URL)
		reviews := len(info.Reviews)
		sig := fmt.Sprintf("%s/%s/c%d/r%d", info.State, info.ReviewDecision, comments, reviews)
		prev := w.st.PRStates[pr.URL]
		if prev == sig {
			continue
		}
		first := prev == ""
		prevComments := parsePrevComments(prev)
		w.st.PRStates[pr.URL] = sig
		w.saveState(m)
		if first {
			continue // baseline, not news
		}

		switch {
		case info.State == "MERGED":
			m.AppendEvent("pr_merged", "system", map[string]any{"url": pr.URL})
		case info.ReviewDecision == "CHANGES_REQUESTED":
			m.AppendEvent("pr_changes_requested", "system", map[string]any{"url": pr.URL})
		case comments > prevComments:
			m.AppendEvent("pr_comments", "system", map[string]any{
				"url": pr.URL, "detail": fmt.Sprintf("%d new comment(s) — now %d comments, %d reviews", comments-prevComments, comments, reviews),
			})
		default:
			m.AppendEvent("pr_activity", "system", map[string]any{
				"url": pr.URL, "detail": fmt.Sprintf("%d comments, %d reviews, decision %s", comments, reviews, orDash(info.ReviewDecision)),
			})
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// prReviewCommentCount returns the number of inline (code-line) review
// comments — the kind gh pr view --json comments omits. Best-effort: 0 on any
// failure, so detection degrades to conversation comments only.
func prReviewCommentCount(url string) int {
	owner, repo, num := parsePRURL(url)
	if num == "" {
		return 0
	}
	out, err := exec.Command("gh", "api", "--paginate",
		fmt.Sprintf("repos/%s/%s/pulls/%s/comments", owner, repo, num), "--jq", "length").Output()
	if err != nil {
		return 0
	}
	total := 0
	for _, line := range strings.Fields(string(out)) { // --paginate emits one length per page
		if n, err := strconv.Atoi(line); err == nil {
			total += n
		}
	}
	return total
}

var prURLRe2 = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

func parsePRURL(url string) (owner, repo, num string) {
	if mm := prURLRe2.FindStringSubmatch(url); mm != nil {
		return mm[1], mm[2], mm[3]
	}
	return "", "", ""
}

// parsePrevComments extracts the comment count from a prior signature
// ("STATE/DECISION/cN/rM") so a rise can be told from a fall.
func parsePrevComments(sig string) int {
	for _, part := range strings.Split(sig, "/") {
		if n, ok := strings.CutPrefix(part, "c"); ok {
			if v, err := strconv.Atoi(n); err == nil {
				return v
			}
		}
	}
	return 0
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
		cost, ctxTokens, lastUser := w.tailSession(m.Dir(), sid, m.Orchestrator.Model)
		if !lastUser.IsZero() {
			w.st.OrcLastUserAt = lastUser
		}
		if cost > 0 {
			total += cost
			appendLedger(m, "orchestrator", "orchestrator", m.Orchestrator.Model, cost)
		}
		if ctxTokens > 0 {
			w.adviseCompaction(m, ctxTokens)
		}
	}

	reg, err := harness.LoadRegistry(m)
	if err == nil {
		for _, worker := range reg.Workers {
			workerCost := 0.0
			for _, sid := range worker.SessionIDs {
				cost, _, _ := w.tailSession(worker.WorktreePath, sid, worker.Model)
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

func (w *Watcher) tailSession(cwd, sessionID, model string) (float64, int64, time.Time) {
	path := costs.TranscriptPath(cwd, sessionID)
	delta, ctxTokens, lastUserAt, newOffset, err := costs.Tail(path, w.st.Offsets[sessionID])
	if err != nil {
		return 0, 0, time.Time{}
	}
	w.st.Offsets[sessionID] = newOffset
	return delta.CostUSD(model), ctxTokens, lastUserAt
}

// adviseCompaction advises the orchestrator ONCE, only when its context is
// genuinely large, that it should checkpoint and /compact at a natural
// boundary. Advisory only — the watcher never types /compact into the pane
// itself; that fights the user and the slash-command menu. The orc (or the
// user) decides when.
func (w *Watcher) adviseCompaction(m *mission.Mission, ctxTokens int64) {
	if ctxTokens < compactAdviseTokens || w.st.ContextWarned {
		return
	}
	w.st.ContextWarned = true
	m.AppendEvent("budget_warning", "system", map[string]any{
		"detail": fmt.Sprintf("orchestrator context ~%dk tokens is large — consider checkpointing and /compact at the next phase boundary (durable state is in the mission files)", ctxTokens/1000),
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

// reapIdleWorkers closes tmux sessions of finished workers after an idle
// grace. The conversation survives on disk — respawn_agent revives it with
// full context — so a parked session costs RAM and tree noise for nothing.
func (w *Watcher) reapIdleWorkers(m *mission.Mission) {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return
	}
	for _, worker := range reg.Workers {
		if !worker.Finished() || !tmuxutil.HasSession(worker.TmuxSession) {
			continue
		}
		idle := time.Since(lastWorkerActivity(worker))
		if idle < reapGrace {
			continue
		}
		tmuxutil.KillSession(worker.TmuxSession)
		fmt.Printf("watcher: reaped %s session (%s, idle %dm)\n", worker.ID, worker.Status, int(idle.Minutes()))
		m.AppendEvent("agent_reaped", "system", map[string]any{"id": worker.ID, "status": worker.Status})
	}
}

// lastWorkerActivity is the newest transcript write across the worker's
// sessions — a finished worker the orchestrator re-engaged for a fix round
// keeps writing its transcript and must not be reaped mid-edit.
func lastWorkerActivity(worker *harness.Worker) time.Time {
	last := worker.SpawnedAt
	if worker.FinishedAt != nil && worker.FinishedAt.After(last) {
		last = *worker.FinishedAt
	}
	for _, sid := range worker.SessionIDs {
		if fi, err := os.Stat(costs.TranscriptPath(worker.WorktreePath, sid)); err == nil && fi.ModTime().After(last) {
			last = fi.ModTime()
		}
	}
	return last
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
	"pr_merged":            true,
	"pr_changes_requested": true,
	"pr_comments":          true,
	"agent_done":           true,
	"agent_blocker":        true,
	"agent_interrupted":    true,
	"panel_done":           true,
	"job_failed":           true,
	"budget_exceeded":      true,
	"merge_failed":         true,
}

// eventNeedsAction: priority types always wake; a standalone job finishing
// wakes too — an orchestrator that launched a background job is usually
// parked on its result. Panel members stay quiet (panel_done covers them).
func eventNeedsAction(ev mission.Event) bool {
	if priorityEvents[ev.Type] {
		return true
	}
	if ev.Type == "job_done" {
		panel, _ := ev.Data["panel"].(string)
		return panel == ""
	}
	return false
}

// pumpDigests wakes the orchestrator when there are unhandled action-needed
// events AND the user is away. Event content is never typed into the
// conversation — the UserPromptSubmit hook attaches it invisibly to whatever
// message arrives next (including our wake-up line, which counts as a prompt).
// A user who is present is never interrupted: their own next message delivers
// everything.
func (w *Watcher) pumpDigests(m *mission.Mission) {
	// The hook delivers events on every orchestrator turn and advances its
	// own cursor; anything at or below it has already been seen. Without
	// this, a wake-up could re-announce events the hook just delivered.
	base := w.st.EventCursor
	if hc := hookCursor(m); hc > base {
		base = hc
		w.st.EventCursor = hc
	}
	events, err := m.EventsSince(base)
	if err != nil || len(events) == 0 {
		return
	}

	needsAction := 0
	maxN := w.st.EventCursor
	headline := ""
	for _, ev := range events {
		if ev.N > maxN {
			maxN = ev.N
		}
		if eventNeedsAction(ev) {
			needsAction++
			if headline == "" {
				headline = eventLine(ev)
			}
		}
	}

	if needsAction == 0 {
		// Informational only — the hook will deliver on the next turn.
		w.st.EventCursor = maxN
		w.saveState(m)
		return
	}

	if !w.userAway() {
		// User active: their next message (any minute now) carries the events;
		// interrupting a present human is the one thing we never do.
		return
	}

	target := m.TmuxSession() + ":orc"
	if !injectSafe(target) {
		if w.holdLogged != maxN {
			w.holdLogged = maxN
			fmt.Printf("watcher: wake-up held — orc pane not inject-safe (%d event(s) pending)\n", needsAction)
		}
		return
	}

	if len(headline) > 110 {
		headline = headline[:110] + "…"
	}
	if needsAction > 1 {
		headline = fmt.Sprintf("%s (+%d more)", headline, needsAction-1)
	}
	msg := fmt.Sprintf("⚡ ox: %s — review the attached events and act per your playbook.", headline)
	if err := harness.SendMessageEnsured(target, msg); err != nil {
		fmt.Printf("watcher: wake-up failed: %v\n", err)
		return
	}
	fmt.Printf("watcher: woke orchestrator (%d action event(s))\n", needsAction)
	cmux.Notify(m, fmt.Sprintf("ox ⚡ %s needs attention", m.ID), headline)
	w.st.EventCursor = maxN
	w.st.LastInjectionAt = time.Now()
	w.saveState(m)
}

// hookCursor is the highest event n the UserPromptSubmit hook has already
// attached to an orchestrator turn (see `ox events attach`).
func hookCursor(m *mission.Mission) int64 {
	data, err := os.ReadFile(filepath.Join(m.Dir(), "hook-cursor"))
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// userAway reports whether the human has been quiet long enough that a
// wake-up is help rather than interruption.
func (w *Watcher) userAway() bool {
	if w.st.OrcLastUserAt.IsZero() {
		return true
	}
	return time.Since(w.st.OrcLastUserAt) > 3*time.Minute
}

// injectSafe: claude prompt is up, nothing streaming, input line empty (the
// user is not mid-typing).
func injectSafe(target string) bool {
	out, err := tmuxutil.CapturePane(target, 15)
	if err != nil {
		return false
	}
	if !strings.Contains(out, "bypass permissions") && !strings.Contains(out, "shift+tab to cycle") {
		return false
	}
	// Streaming does NOT block: claude queues messages typed mid-turn and
	// delivers them when the turn ends — better than holding a wake-up for
	// the length of a long turn. The only true blocker is a non-empty input
	// line (the user mid-typing, or a menu's selection cursor).
	// The input prompt is the last ❯ on screen; whatever renders below it
	// (statusline, hints, artifact chips) is footer chrome that comes and
	// goes with claude releases — enumerating it is a losing game.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "❯") {
			return inputLineEmpty(line)
		}
	}
	return false
}

// inputLineEmpty treats claude's own input-box hints as empty — they render
// on the prompt line but are not typed text.
func inputLineEmpty(promptLine string) bool {
	content := strings.TrimSpace(strings.TrimPrefix(promptLine, "❯"))
	return content == "" || strings.HasPrefix(content, "Press up to edit queued messages")
}

// EventLine renders one event as a user-facing line ("" = internal only).
func EventLine(ev mission.Event) string { return eventLine(ev) }

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
	case "agent_reaped":
		return fmt.Sprintf("%s session closed (finished, idle) — respawn_agent revives it with full context", id)
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
	case "pr_merged":
		return fmt.Sprintf("PR MERGED: %v — wrap up (yoke done + close)", ev.Data["url"])
	case "pr_changes_requested":
		return fmt.Sprintf("PR CHANGES REQUESTED: %v — address the review", ev.Data["url"])
	case "pr_comments":
		return fmt.Sprintf("PR COMMENTS: %v — %v; read them (gh pr view --comments / gh api) and address", ev.Data["url"], ev.Data["detail"])
	case "pr_activity":
		return fmt.Sprintf("PR activity: %v (%v)", ev.Data["url"], ev.Data["detail"])
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
