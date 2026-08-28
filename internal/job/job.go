// Package job runs headless one-shot claude invocations: cheap, parallel,
// exact-cost work (analysis, critique panels, verification, distillation).
// Jobs are detached from every ox process — setsid, output to files, PID in
// the index — so they survive orchestrator, MCP, and watcher restarts.
package job

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/mission"
)

const (
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

type Job struct {
	ID           string     `json:"id"`
	PanelID      string     `json:"panel_id,omitempty"`
	Prompt       string     `json:"-"`
	Model        string     `json:"model"`
	Engine       string     `json:"engine,omitempty"` // "" / "claude" (default) | "opencode"
	CWD          string     `json:"cwd"`
	AddDirs      []string   `json:"add_dirs,omitempty"`
	MaxTurns     int        `json:"max_turns"`
	MaxBudgetUSD float64    `json:"max_budget_usd"`
	ExpectJSON   bool       `json:"expect_json,omitempty"`
	Status       string     `json:"status"`
	PID          int        `json:"pid,omitempty"`
	SessionID    string     `json:"session_id,omitempty"`
	Attempts     int        `json:"attempts"`
	Escalated    bool       `json:"escalated,omitempty"`
	CostUSD      float64    `json:"cost_usd"`
	Error        string     `json:"error,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type Index struct {
	Jobs map[string]*Job `json:"jobs"`
}

func indexPath(m *mission.Mission) string { return filepath.Join(m.Dir(), "jobs.json") }

func LoadIndex(m *mission.Mission) (*Index, error) {
	data, err := os.ReadFile(indexPath(m))
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Jobs: map[string]*Job{}}, nil
		}
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse jobs.json: %w", err)
	}
	if idx.Jobs == nil {
		idx.Jobs = map[string]*Job{}
	}
	return &idx, nil
}

func UpdateIndex(oxHome string, m *mission.Mission, fn func(*Index) error) error {
	return mission.WithLock(oxHome, m.ID, func() error {
		idx, err := LoadIndex(m)
		if err != nil {
			return err
		}
		if err := fn(idx); err != nil {
			return err
		}
		data, err := json.MarshalIndent(idx, "", "  ")
		if err != nil {
			return err
		}
		tmp := indexPath(m) + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, indexPath(m))
	})
}

type StartInput struct {
	ID           string
	PanelID      string
	Prompt       string
	Model        string
	Engine       string // "" / "claude" (default) | "opencode"
	CWD          string // "" = mission dir | "repo:<name>" = base clone | absolute path
	AddDirs      []string
	MaxTurns     int
	MaxBudgetUSD float64
	ExpectJSON   bool
}

// Start registers and launches a detached headless job.
func Start(cfg *config.Config, m *mission.Mission, in StartInput) (*Job, error) {
	if strings.TrimSpace(in.Prompt) == "" {
		return nil, fmt.Errorf("prompt required")
	}
	if m.SpendFrozen && cfg.Budgets.Enforce {
		return nil, fmt.Errorf("mission spend is frozen — raise the budget first")
	}
	if in.ID == "" {
		in.ID = fmt.Sprintf("job-%s", time.Now().Format("150405")) + "-" + uuid.NewString()[:4]
	}
	if in.Model == "" {
		// opencode addresses models as provider/model and has no claude-tier
		// default to fall back on — the caller must name one.
		if in.Engine == "opencode" {
			return nil, fmt.Errorf("opencode job requires an explicit provider/model (e.g. openrouter/z-ai/glm-4.7-flash)")
		}
		in.Model = cfg.JobModel()
	}
	// Jobs that read real code die around 15 turns — the audit trail is a
	// graveyard of error_max_turns at 8-25. Default generously; floor
	// explicit lowballs so an orchestrator's anchor bias can't starve a job.
	if in.MaxTurns == 0 {
		in.MaxTurns = 40
	} else if in.MaxTurns < 20 {
		in.MaxTurns = 20
	}
	if in.MaxBudgetUSD == 0 {
		in.MaxBudgetUSD = m.Budgets.PerJobUSD
	}

	j := &Job{
		ID: in.ID, PanelID: in.PanelID, Model: in.Model, Engine: in.Engine, CWD: in.CWD,
		AddDirs: in.AddDirs, MaxTurns: in.MaxTurns, MaxBudgetUSD: in.MaxBudgetUSD,
		ExpectJSON: in.ExpectJSON, Status: StatusRunning, Attempts: 1, StartedAt: time.Now(),
	}

	if err := UpdateIndex(cfg.Home, m, func(idx *Index) error {
		if _, exists := idx.Jobs[j.ID]; exists {
			return fmt.Errorf("job %q already exists", j.ID)
		}
		idx.Jobs[j.ID] = j
		return nil
	}); err != nil {
		return nil, err
	}

	if err := os.WriteFile(jobFile(m, j.ID, "prompt.md"), []byte(in.Prompt), 0o644); err != nil {
		return nil, err
	}

	if err := launch(cfg, m, j); err != nil {
		markFailed(cfg.Home, m, j, "launch: "+err.Error())
		return nil, err
	}
	m.AppendEvent("job_started", "orchestrator", map[string]any{"id": j.ID, "model": j.Model, "panel": j.PanelID})
	return j, nil
}

func launch(cfg *config.Config, m *mission.Mission, j *Job) error {
	if j.Engine == "opencode" {
		return launchOpencode(cfg, m, j)
	}

	j.SessionID = uuid.NewString()

	args := []string{
		"-p", "--dangerously-skip-permissions",
		"--output-format", "json",
		"--model", j.Model,
		"--max-turns", fmt.Sprintf("%d", j.MaxTurns),
		"--session-id", j.SessionID,
		"--strict-mcp-config",
	}
	// The native headless budget flag is part of budget ENFORCEMENT — it
	// obeys the same global switch as the watcher's checks. Tracking (exact
	// cost from the result JSON) happens regardless.
	if cfg.Budgets.Enforce && j.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", j.MaxBudgetUSD))
	}
	for _, d := range j.AddDirs {
		args = append(args, "--add-dir", d)
	}
	return spawnDetached(cfg, m, j, "claude", args, nil)
}

// launchOpencode runs a job on opencode instead of claude. opencode reads the
// prompt from stdin and streams newline-delimited JSON events to stdout
// (--format json); there are no native --max-turns/--max-budget flags, so
// opencode governs its own loop. The session id is server-assigned and
// discovered from the transcript at harvest, not pre-assigned here. Provider
// keys come from ox secrets injected into the child's env.
func launchOpencode(cfg *config.Config, m *mission.Mission, j *Job) error {
	args := []string{"run", "--format", "json", "-m", j.Model}
	return spawnDetached(cfg, m, j, "opencode", args, oxSecretsEnv())
}

// spawnDetached wires the shared job plumbing for either engine: prompt on
// stdin, output.json on stdout, job.log on stderr, setsid so the child
// outlives every ox process, and PID capture into the index.
func spawnDetached(cfg *config.Config, m *mission.Mission, j *Job, name string, args, extraEnv []string) error {
	promptF, err := os.Open(jobFile(m, j.ID, "prompt.md"))
	if err != nil {
		return err
	}
	defer promptF.Close()
	outF, err := os.Create(jobFile(m, j.ID, "output.json"))
	if err != nil {
		return err
	}
	defer outF.Close()
	logF, err := os.Create(jobFile(m, j.ID, "job.log"))
	if err != nil {
		return err
	}
	defer logF.Close()

	cmd := exec.Command(name, args...)
	cmd.Dir = jobCWD(cfg, m, j)
	cmd.Stdin = promptF
	cmd.Stdout = outF
	cmd.Stderr = logF
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	j.PID = cmd.Process.Pid
	// Reap on exit — without Wait the child zombifies while this process
	// lives, and kill(pid,0) keeps reporting it alive. Setsid already
	// guarantees the job survives us if we die first (init reaps then).
	go cmd.Wait()

	return UpdateIndex(cfg.Home, m, func(idx *Index) error {
		if cur := idx.Jobs[j.ID]; cur != nil {
			cur.PID = j.PID
			cur.SessionID = j.SessionID
			cur.Status = StatusRunning
		}
		return nil
	})
}

// jobCWD resolves where a job runs: mission dir by default, a base repo clone
// for "repo:<name>", or an explicit absolute path.
func jobCWD(cfg *config.Config, m *mission.Mission, j *Job) string {
	switch {
	case strings.HasPrefix(j.CWD, "repo:"):
		return filepath.Join(cfg.Home, "repos", strings.TrimPrefix(j.CWD, "repo:"))
	case j.CWD != "" && filepath.IsAbs(j.CWD):
		return j.CWD
	}
	return m.Dir()
}

// oxSecretsEnv reads ~/.ox/secrets.env into KEY=VALUE entries for a child's
// env. opencode resolves {env:OPENROUTER_API_KEY} etc. against its process
// env, and a detached job doesn't inherit the interactive shell that sources
// secrets, so we inject them here.
func oxSecretsEnv() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".ox", "secrets.env"))
	if err != nil {
		return nil
	}
	var env []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env = append(env, key+"="+strings.Trim(val, `"'`))
	}
	return env
}

// headlessResult is the claude -p --output-format json envelope.
type headlessResult struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns     int     `json:"num_turns"`
	DurationMS   int64   `json:"duration_ms"`
}

// Harvest reconciles one job. The output envelope is the primary completion
// signal (a complete result JSON means done even if the PID lingers); a dead
// PID with no parseable output means failure. Idempotent. Returns true when
// the job just reached a terminal state.
func Harvest(cfg *config.Config, m *mission.Mission, j *Job) (bool, error) {
	if j.Status != StatusRunning {
		return false, nil
	}
	if j.Engine == "opencode" {
		return harvestOpencode(cfg, m, j)
	}

	res, err := parseResult(jobFile(m, j.ID, "output.json"))
	if err != nil {
		if j.PID > 0 && processAlive(j.PID) {
			return false, nil
		}
		logTail := readTail(jobFile(m, j.ID, "job.log"), 400)
		markFailed(cfg.Home, m, j, fmt.Sprintf("no parseable result (%v) %s", err, logTail))
		m.AppendEvent("job_failed", "system", map[string]any{"id": j.ID, "error": j.Error})
		return true, nil
	}

	if res.IsError {
		markFailed(cfg.Home, m, j, fmt.Sprintf("%s: %.300s", res.Subtype, res.Result))
		recordCost(cfg.Home, m, j, res.TotalCostUSD)
		m.AppendEvent("job_failed", "system", map[string]any{"id": j.ID, "error": res.Subtype})
		return true, nil
	}

	if j.ExpectJSON && !looksLikeJSON(res.Result) {
		markFailed(cfg.Home, m, j, "output contract violated: expected JSON result")
		recordCost(cfg.Home, m, j, res.TotalCostUSD)
		m.AppendEvent("job_failed", "system", map[string]any{"id": j.ID, "error": "contract"})
		return true, nil
	}

	now := time.Now()
	UpdateIndex(cfg.Home, m, func(idx *Index) error {
		if cur := idx.Jobs[j.ID]; cur != nil {
			cur.Status = StatusDone
			cur.FinishedAt = &now
			cur.CostUSD += res.TotalCostUSD
			*j = *cur
		}
		return nil
	})
	recordLedger(m, j, res.TotalCostUSD)
	m.AppendEvent("job_done", "system", map[string]any{"id": j.ID, "cost_usd": res.TotalCostUSD, "panel": j.PanelID})
	return true, nil
}

// harvestOpencode reconciles an opencode job. opencode streams JSON events for
// the whole run, so — unlike claude, where a complete result envelope is the
// done signal — the transcript carries content long before the job finishes.
// The process exiting is the only reliable done signal, so wait for the PID to
// clear before treating the transcript as final.
func harvestOpencode(cfg *config.Config, m *mission.Mission, j *Job) (bool, error) {
	if j.PID == 0 || processAlive(j.PID) {
		return false, nil
	}

	res, sess, err := parseOpencodeResult(jobFile(m, j.ID, "output.json"))
	if err != nil {
		logTail := readTail(jobFile(m, j.ID, "job.log"), 400)
		markFailed(cfg.Home, m, j, fmt.Sprintf("no parseable result (%v) %s", err, logTail))
		m.AppendEvent("job_failed", "system", map[string]any{"id": j.ID, "error": j.Error})
		return true, nil
	}

	if res.IsError {
		markFailed(cfg.Home, m, j, fmt.Sprintf("%.300s", res.Result))
		recordCost(cfg.Home, m, j, res.TotalCostUSD)
		m.AppendEvent("job_failed", "system", map[string]any{"id": j.ID, "error": "opencode"})
		return true, nil
	}

	if j.ExpectJSON && !looksLikeJSON(res.Result) {
		markFailed(cfg.Home, m, j, "output contract violated: expected JSON result")
		recordCost(cfg.Home, m, j, res.TotalCostUSD)
		m.AppendEvent("job_failed", "system", map[string]any{"id": j.ID, "error": "contract"})
		return true, nil
	}

	now := time.Now()
	UpdateIndex(cfg.Home, m, func(idx *Index) error {
		if cur := idx.Jobs[j.ID]; cur != nil {
			cur.Status = StatusDone
			cur.FinishedAt = &now
			cur.CostUSD += res.TotalCostUSD
			if sess != "" {
				cur.SessionID = sess
			}
			*j = *cur
		}
		return nil
	})
	recordLedger(m, j, res.TotalCostUSD)
	m.AppendEvent("job_done", "system", map[string]any{"id": j.ID, "cost_usd": res.TotalCostUSD, "panel": j.PanelID})
	return true, nil
}

// HarvestAll reconciles every running job; returns jobs that just finished.
func HarvestAll(cfg *config.Config, m *mission.Mission) []*Job {
	idx, err := LoadIndex(m)
	if err != nil {
		return nil
	}
	var finished []*Job
	for _, j := range idx.Jobs {
		if done, _ := Harvest(cfg, m, j); done {
			finished = append(finished, j)
		}
	}
	return finished
}

// RetryOrEscalate relaunches a failed job — once on the same model, then one
// tier up (haiku→sonnet). Returns the updated job or nil when out of retries.
func RetryOrEscalate(cfg *config.Config, m *mission.Mission, j *Job) (*Job, error) {
	if j.Status != StatusFailed {
		return nil, fmt.Errorf("job %s is not failed", j.ID)
	}
	switch {
	case j.Attempts < 2:
		// retry same model
	case j.Attempts == 2 && j.Model == "haiku":
		j.Model = "sonnet"
		j.Escalated = true
	default:
		return nil, nil
	}

	if err := UpdateIndex(cfg.Home, m, func(idx *Index) error {
		if cur := idx.Jobs[j.ID]; cur != nil {
			cur.Attempts++
			cur.Model = j.Model
			cur.Escalated = j.Escalated
			cur.Status = StatusRunning
			cur.Error = ""
			cur.FinishedAt = nil
			*j = *cur
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := launch(cfg, m, j); err != nil {
		markFailed(cfg.Home, m, j, "relaunch: "+err.Error())
		return nil, err
	}
	m.AppendEvent("job_started", "system", map[string]any{"id": j.ID, "model": j.Model, "attempt": j.Attempts})
	return j, nil
}

// Result returns the result text of a done job.
func Result(m *mission.Mission, j *Job) (string, error) {
	if j.Engine == "opencode" {
		res, _, err := parseOpencodeResult(jobFile(m, j.ID, "output.json"))
		if err != nil {
			return "", err
		}
		return res.Result, nil
	}
	res, err := parseResult(jobFile(m, j.ID, "output.json"))
	if err != nil {
		return "", err
	}
	return res.Result, nil
}

// opencodeEvent is one line of `opencode run --format json` (JSONL). Only the
// fields the harvester consumes are modeled.
type opencodeEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Error     json.RawMessage `json:"error,omitempty"`
	Part      struct {
		Type string  `json:"type"`
		Text string  `json:"text"`
		Cost float64 `json:"cost"`
	} `json:"part"`
}

// parseOpencodeResult folds an opencode JSONL transcript into the same result
// shape the rest of the harvester expects: assistant text concatenated, cost
// summed across steps, plus the server-assigned session id (for resume).
func parseOpencodeResult(path string) (*headlessResult, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var text strings.Builder
	var cost float64
	var sessionID, errText string
	var sawEvent, isErr bool
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev opencodeEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		sawEvent = true
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		switch ev.Type {
		case "text":
			text.WriteString(ev.Part.Text)
		case "step_finish":
			cost += ev.Part.Cost
		case "error":
			isErr = true
			errText = string(ev.Error)
		}
	}
	if !sawEvent {
		return nil, "", fmt.Errorf("empty output")
	}
	result := text.String()
	if result == "" && !isErr {
		isErr, errText = true, "opencode produced no result text"
	}
	if isErr && result == "" {
		result = errText
	}
	return &headlessResult{Type: "result", Result: result, TotalCostUSD: cost, IsError: isErr}, sessionID, nil
}

func markFailed(oxHome string, m *mission.Mission, j *Job, msg string) {
	now := time.Now()
	UpdateIndex(oxHome, m, func(idx *Index) error {
		if cur := idx.Jobs[j.ID]; cur != nil {
			cur.Status = StatusFailed
			cur.Error = msg
			cur.FinishedAt = &now
			*j = *cur
		}
		return nil
	})
}

func recordCost(oxHome string, m *mission.Mission, j *Job, cost float64) {
	if cost <= 0 {
		return
	}
	UpdateIndex(oxHome, m, func(idx *Index) error {
		if cur := idx.Jobs[j.ID]; cur != nil {
			cur.CostUSD += cost
		}
		return nil
	})
	recordLedger(m, j, cost)
}

// recordLedger appends the exact job cost and rolls the mission total up.
func recordLedger(m *mission.Mission, j *Job, cost float64) {
	if cost <= 0 {
		return
	}
	entry, _ := json.Marshal(map[string]any{
		"ts": time.Now().Format(time.RFC3339), "actor": j.ID, "kind": "job",
		"model": j.Model, "cost_usd": cost, "source": "exact",
	})
	f, err := os.OpenFile(filepath.Join(m.Dir(), "ledger.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		f.Write(append(entry, '\n'))
		f.Close()
	}
	mission.Update(missionHome(m), m.ID, func(mm *mission.Mission) error {
		mm.SpentUSD += cost
		return nil
	})
}

// missionHome derives oxHome from the mission dir (…/missions/<slug>).
func missionHome(m *mission.Mission) string {
	return filepath.Dir(filepath.Dir(m.Dir()))
}

func parseResult(path string) (*headlessResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("empty output")
	}
	var res headlessResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("parse output.json: %w", err)
	}
	if res.Type != "result" {
		return nil, fmt.Errorf("unexpected envelope type %q", res.Type)
	}
	return &res, nil
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func looksLikeJSON(s string) bool {
	s = strings.TrimSpace(s)
	// Models often wrap JSON in fences; accept that.
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSpace(strings.TrimSuffix(s, "```"))
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func readTail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

func jobFile(m *mission.Mission, jobID, name string) string {
	dir := filepath.Join(m.Dir(), "jobs", jobID)
	os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, name)
}
