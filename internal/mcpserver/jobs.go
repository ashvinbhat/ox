package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/personas"
)

type runJobIn struct {
	ID           string   `json:"id,omitempty" jsonschema:"optional job id; auto-generated when empty"`
	Prompt       string   `json:"prompt" jsonschema:"the complete self-contained prompt; the job has no mission context beyond this"`
	Persona      string   `json:"persona,omitempty" jsonschema:"prepend this persona's instructions and inherit its model/output contract (e.g. reviewer-security)"`
	Model        string   `json:"model,omitempty" jsonschema:"claude engine: haiku (default) | sonnet | opus. opencode engine: a provider/model id (e.g. openrouter/z-ai/glm-4.7-flash) — required"`
	Engine       string   `json:"engine,omitempty" jsonschema:"'claude' (default) | 'opencode' — only set opencode when the user asked to run this on another model"`
	CWD          string   `json:"cwd,omitempty" jsonschema:"'repo:<name>' runs in the base clone; absolute path; default mission dir"`
	AddDirs      []string `json:"add_dirs,omitempty" jsonschema:"extra directories the job may read"`
	MaxTurns     int      `json:"max_turns,omitempty" jsonschema:"default 40, floor 20 — omit unless deliberately constraining; jobs that read code need 30+"`
	MaxBudgetUSD float64  `json:"max_budget_usd,omitempty"`
	ExpectJSON   bool     `json:"expect_json,omitempty" jsonschema:"fail the job if the result is not JSON"`
	Wait         bool     `json:"wait,omitempty" jsonschema:"block until the job finishes (includes auto retry/escalation)"`
	TimeoutS     int      `json:"timeout_s,omitempty" jsonschema:"wait deadline, default 600"`
}

type jobState struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Model     string  `json:"model"`
	CostUSD   float64 `json:"cost_usd"`
	Error     string  `json:"error,omitempty"`
	Result    string  `json:"result,omitempty"`
	Escalated bool    `json:"escalated,omitempty"`
}

type runPanelIn struct {
	PanelID  string     `json:"panel_id,omitempty"`
	Jobs     []runJobIn `json:"jobs" jsonschema:"the panel's parallel jobs"`
	Wait     bool       `json:"wait,omitempty" jsonschema:"block until every panel job finishes"`
	TimeoutS int        `json:"timeout_s,omitempty" jsonschema:"wait deadline, default 900"`
}
type runPanelOut struct {
	PanelID string     `json:"panel_id"`
	Jobs    []jobState `json:"jobs"`
}

type jobStatusIn struct {
	JobID   string `json:"job_id,omitempty"`
	PanelID string `json:"panel_id,omitempty"`
}
type jobStatusOut struct {
	Jobs []jobState `json:"jobs"`
}

type jobResultIn struct {
	JobID string `json:"job_id"`
}

func (s *Server) registerJobTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_job",
		Description: "Run a headless one-shot job (analysis, verification, summarization). Cheap claude model by default, exact cost, survives restarts. Prefer this over spawning an agent for read-mostly questions. Set engine=opencode (with a provider/model) only when the user asked to run on another model.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in runJobIn) (*mcp.CallToolResult, jobState, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, jobState{}, err
		}
		j, err := job.Start(s.cfg, m, s.startInput(in, ""))
		if err != nil {
			return nil, jobState{}, err
		}
		if !in.Wait {
			return nil, toState(m, j, false), nil
		}
		timeout := time.Duration(defaultInt(in.TimeoutS, 600)) * time.Second
		final := s.awaitJob(ctx, m, j, timeout)
		return nil, toState(m, final, true), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_panel",
		Description: "Fan out parallel jobs (plan critique, review perspectives, parallel log scans) under one panel id.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in runPanelIn) (*mcp.CallToolResult, runPanelOut, error) {
		if len(in.Jobs) == 0 {
			return nil, runPanelOut{}, fmt.Errorf("jobs required")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, runPanelOut{}, err
		}
		panelID := in.PanelID
		if panelID == "" {
			panelID = "panel-" + time.Now().Format("150405")
		}

		var jobs []*job.Job
		for i, spec := range in.Jobs {
			if spec.ID == "" {
				spec.ID = fmt.Sprintf("%s-%d", panelID, i+1)
			}
			j, err := job.Start(s.cfg, m, s.startInput(spec, panelID))
			if err != nil {
				return nil, runPanelOut{}, fmt.Errorf("job %s: %w", spec.ID, err)
			}
			jobs = append(jobs, j)
		}

		out := runPanelOut{PanelID: panelID}
		if in.Wait {
			timeout := time.Duration(defaultInt(in.TimeoutS, 900)) * time.Second
			deadline := time.Now().Add(timeout)
			for _, j := range jobs {
				remaining := time.Until(deadline)
				if remaining <= 0 {
					remaining = time.Second
				}
				final := s.awaitJob(ctx, m, j, remaining)
				out.Jobs = append(out.Jobs, toState(m, final, true))
			}
			m.AppendEvent("panel_done", "system", map[string]any{"panel": panelID})
		} else {
			for _, j := range jobs {
				out.Jobs = append(out.Jobs, toState(m, j, false))
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "job_status",
		Description: "Status of one job, a panel, or all jobs.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in jobStatusIn) (*mcp.CallToolResult, jobStatusOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, jobStatusOut{}, err
		}
		job.HarvestAll(s.cfg, m)
		idx, err := job.LoadIndex(m)
		if err != nil {
			return nil, jobStatusOut{}, err
		}
		var out jobStatusOut
		for _, j := range idx.Jobs {
			if in.JobID != "" && j.ID != in.JobID {
				continue
			}
			if in.PanelID != "" && j.PanelID != in.PanelID {
				continue
			}
			out.Jobs = append(out.Jobs, toState(m, j, false))
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "job_result",
		Description: "The result text of a finished job (never the transcript).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in jobResultIn) (*mcp.CallToolResult, jobState, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, jobState{}, err
		}
		idx, err := job.LoadIndex(m)
		if err != nil {
			return nil, jobState{}, err
		}
		j, ok := idx.Jobs[in.JobID]
		if !ok {
			return nil, jobState{}, fmt.Errorf("job %q not found", in.JobID)
		}
		job.Harvest(s.cfg, m, j)
		return nil, toState(m, j, true), nil
	})
}

// awaitJob polls until terminal, applying the retry→escalate policy on
// failure. Returns the final job state.
func (s *Server) awaitJob(ctx context.Context, m *mission.Mission, j *job.Job, timeout time.Duration) *job.Job {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return j
		case <-time.After(2 * time.Second):
		}
		job.Harvest(s.cfg, m, j)
		switch j.Status {
		case job.StatusDone:
			return j
		case job.StatusFailed:
			retried, err := job.RetryOrEscalate(s.cfg, m, j)
			if err != nil || retried == nil {
				return j
			}
			j = retried
		}
	}
	return j
}

func (s *Server) startInput(in runJobIn, panelID string) job.StartInput {
	prompt := in.Prompt
	if in.Persona != "" {
		personas.EnsureEmbeddedDefaults(s.oxHome)
		if reg, err := personas.LoadRegistry(s.oxHome); err == nil {
			if p, ok := reg.Get(in.Persona); ok {
				prompt = p.Content + "\n\n---\n\n" + in.Prompt
				if in.Model == "" {
					in.Model = p.DefaultModel
				}
				if in.MaxTurns == 0 && p.MaxTurns > 0 {
					in.MaxTurns = p.MaxTurns
				}
				if in.MaxBudgetUSD == 0 && p.MaxBudgetUSD > 0 {
					in.MaxBudgetUSD = p.MaxBudgetUSD
				}
				if p.Output == "findings_json" {
					in.ExpectJSON = true
				}
			}
		}
	}
	return job.StartInput{
		ID: in.ID, PanelID: panelID, Prompt: prompt, Model: in.Model, Engine: in.Engine, CWD: in.CWD,
		AddDirs: in.AddDirs, MaxTurns: in.MaxTurns, MaxBudgetUSD: in.MaxBudgetUSD, ExpectJSON: in.ExpectJSON,
	}
}

func toState(m *mission.Mission, j *job.Job, includeResult bool) jobState {
	st := jobState{ID: j.ID, Status: j.Status, Model: j.Model, CostUSD: j.CostUSD, Error: j.Error, Escalated: j.Escalated}
	if includeResult && j.Status == job.StatusDone {
		if res, err := job.Result(m, j); err == nil {
			st.Result = res
		}
	}
	return st
}

func defaultInt(v, d int) int {
	if v > 0 {
		return v
	}
	return d
}
