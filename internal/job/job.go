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
		in.Model = cfg.JobModel()
	}
	if in.MaxTurns == 0 {
		in.MaxTurns = 15
	}
	if in.MaxBudgetUSD == 0 {
		in.MaxBudgetUSD = m.Budgets.PerJobUSD
	}

	j := &Job{
		ID: in.ID, PanelID: in.PanelID, Model: in.Model, CWD: in.CWD,
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
	j.SessionID = uuid.NewString()

	args := []string{
		"-p", "--dangerously-skip-permissions",
		"--output-format", "json",
		"--model", j.Model,
		"--max-turns", fmt.Sprintf("%d", j.MaxTurns),
		"--max-budget-usd", fmt.Sprintf("%.2f", j.MaxBudgetUSD),
		"--session-id", j.SessionID,
		"--strict-mcp-config",
	}
	for _, d := range j.AddDirs {
		args = append(args, "--add-dir", d)
	}

	cwd := m.Dir()
	switch {
	case strings.HasPrefix(j.CWD, "repo:"):
		cwd = filepath.Join(cfg.Home, "repos", strings.TrimPrefix(j.CWD, "repo:"))
	case j.CWD != "" && filepath.IsAbs(j.CWD):
		cwd = j.CWD
	}

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

	cmd := exec.Command("claude", args...)
	cmd.Dir = cwd
	cmd.Stdin = promptF
	cmd.Stdout = outF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude: %w", err)
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
	res, err := parseResult(jobFile(m, j.ID, "output.json"))
	if err != nil {
		return "", err
	}
	return res.Result, nil
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
