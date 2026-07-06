// Package mcpserver is the typed tool surface the orchestrator and workers
// use to drive the harness. One stdio server process runs per claude session
// (spawned via --mcp-config); it holds no state between calls — everything
// loads from and saves to the mission file store, so a crashed server
// restarts clean.
package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ashvinbhat/ox/internal/checkpoint"
	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/ashvinbhat/ox/internal/memory/embed"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/scratchpad"
	"github.com/ashvinbhat/ox/internal/watcher"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

const (
	RoleOrchestrator = "orchestrator"
	RoleWorker       = "worker"
)

type Server struct {
	oxHome    string
	missionID string
	role      string
	agentID   string
	cfg       *config.Config
}

// Run serves MCP over stdio until the client (the claude session) goes away.
func Run(cfg *config.Config, missionID, role, agentID string) error {
	if role != RoleOrchestrator && role != RoleWorker {
		return fmt.Errorf("role must be %s or %s", RoleOrchestrator, RoleWorker)
	}
	if _, err := mission.Open(cfg.Home, missionID); err != nil {
		return err
	}

	s := &Server{oxHome: cfg.Home, missionID: missionID, role: role, agentID: agentID, cfg: cfg}

	srv := mcp.NewServer(&mcp.Implementation{Name: "ox", Version: "2.0.0"}, nil)
	s.registerCommon(srv)
	if role == RoleOrchestrator {
		s.registerOrchestrator(srv)
		s.registerAgentTools(srv)
		s.registerJobTools(srv)
	} else {
		s.registerWorker(srv)
	}

	return srv.Run(context.Background(), &mcp.StdioTransport{})
}

func (s *Server) actor() string {
	if s.role == RoleOrchestrator {
		return "orchestrator"
	}
	return "worker:" + s.agentID
}

func (s *Server) openMission() (*mission.Mission, error) {
	return mission.Open(s.oxHome, s.missionID)
}

func (s *Server) memoryStore() (*memory.Store, error) {
	return memory.Open(s.oxHome, embed.New(s.cfg.Memory.Embeddings))
}

// missionScopes is the default recall scope set: mission repos + task + global.
func (s *Server) missionScopes(m *mission.Mission) []string {
	var scopes []string
	for repo := range m.Repos {
		scopes = append(scopes, "repo:"+repo)
	}
	if m.Yoke != nil {
		scopes = append(scopes, fmt.Sprintf("task:%d", m.Yoke.Seq))
	}
	return scopes
}

// ---- common tools (all roles) ----

type scratchPostIn struct {
	Content string `json:"content" jsonschema:"the note to post"`
	Kind    string `json:"kind,omitempty" jsonschema:"discovery | question | decision | blocker (default discovery)"`
}
type emptyOut struct {
	OK bool `json:"ok"`
}

type scratchReadIn struct {
	Since string `json:"since,omitempty" jsonschema:"only entries newer than this duration, e.g. 2h, 30m"`
}
type scratchReadOut struct {
	Content string `json:"content"`
}

type checkpointIn struct {
	Done      string   `json:"done" jsonschema:"what was completed"`
	Next      string   `json:"next,omitempty" jsonschema:"what happens next"`
	Decisions []string `json:"decisions,omitempty"`
}
type checkpointOut struct {
	CheckpointID string `json:"checkpoint_id"`
}

type recallIn struct {
	Query           string   `json:"query" jsonschema:"natural-language query"`
	Scope           string   `json:"scope,omitempty" jsonschema:"'global' | 'repo:<name>' | 'task:<seq>'; default: this mission's repos + task + global"`
	Kinds           []string `json:"kinds,omitempty" jsonschema:"filter: learning|gotcha|convention|architecture|decision|tool|profile|failure"`
	K               int      `json:"k,omitempty" jsonschema:"max results, default 8"`
	IncludeArchived bool     `json:"include_archived,omitempty"`
}
type recallOut struct {
	Memories []memory.Memory `json:"memories"`
	Degraded string          `json:"degraded,omitempty" jsonschema:"'fts_only' when embeddings are unavailable"`
}

type rememberIn struct {
	Content    string   `json:"content" jsonschema:"self-contained knowledge, useful to an agent with no mission context; max ~120 tokens"`
	Kind       string   `json:"kind" jsonschema:"learning|gotcha|convention|architecture|decision|tool|profile|failure"`
	Scope      string   `json:"scope,omitempty" jsonschema:"'global' | 'repo:<name>' | 'task:<seq>'; default repo of this mission or global"`
	Title      string   `json:"title,omitempty" jsonschema:"max 80 chars; defaults to the first sentence"`
	Tags       []string `json:"tags,omitempty"`
	Supersedes string   `json:"supersedes,omitempty" jsonschema:"uid of a memory this replaces or corrects"`
}

func (s *Server) registerCommon(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "post_scratch",
		Description: "Post to the shared mission scratchpad (discoveries, questions, decisions, blockers). All mission participants read it.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in scratchPostIn) (*mcp.CallToolResult, emptyOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, emptyOut{}, err
		}
		kind := in.Kind
		if kind == "" {
			kind = "discovery"
		}
		sp := scratchpad.New(m.Dir())
		if err := sp.Append(scratchpad.Entry{AgentID: s.actor(), Kind: kind, Content: in.Content}); err != nil {
			return nil, emptyOut{}, err
		}
		if kind == "decision" {
			appendDecision(m.Dir(), s.actor(), in.Content)
		}
		m.AppendEvent("scratch_posted", s.actor(), map[string]any{"kind": kind})
		return nil, emptyOut{OK: true}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_scratch",
		Description: "Read the shared mission scratchpad, optionally only recent entries.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in scratchReadIn) (*mcp.CallToolResult, scratchReadOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, scratchReadOut{}, err
		}
		sp := scratchpad.New(m.Dir())
		if in.Since != "" {
			d, err := time.ParseDuration(in.Since)
			if err != nil {
				return nil, scratchReadOut{}, fmt.Errorf("bad duration %q", in.Since)
			}
			content, err := sp.ReadSince(time.Now().Add(-d))
			return nil, scratchReadOut{Content: content}, err
		}
		content, err := sp.Read()
		return nil, scratchReadOut{Content: content}, err
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "checkpoint",
		Description: "Save a progress checkpoint (done / next / decisions). Use at phase transitions and before risky steps.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in checkpointIn) (*mcp.CallToolResult, checkpointOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, checkpointOut{}, err
		}
		mgr := checkpoint.NewManager(m.Dir(), m.ID)
		cp, err := mgr.Create(in.Done, in.Next, in.Decisions)
		if err != nil {
			return nil, checkpointOut{}, err
		}
		mission.Update(s.oxHome, s.missionID, func(mm *mission.Mission) error {
			mm.Checkpoint = mission.Checkpoint{At: time.Now(), Done: in.Done, Next: in.Next}
			return nil
		})
		m.AppendEvent("checkpoint", s.actor(), map[string]any{"id": cp.ID, "done": in.Done})
		if m.Yoke != nil {
			yokecli.AddNote(m.Yoke.ID, cp.ToYokeNote())
		}
		return nil, checkpointOut{CheckpointID: cp.ID}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "recall",
		Description: "Search ox long-term memory (learnings, gotchas, conventions, architecture facts). Use before exploring from scratch or when something behaves unexpectedly.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in recallIn) (*mcp.CallToolResult, recallOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, recallOut{}, err
		}
		store, err := s.memoryStore()
		if err != nil {
			return nil, recallOut{}, err
		}
		defer store.Close()

		scopes := s.missionScopes(m)
		if in.Scope != "" {
			scopes = []string{in.Scope}
		}
		mems, degraded, err := store.Search(ctx, in.Query, memory.SearchOptions{
			Scopes: scopes, Kinds: in.Kinds, K: in.K, IncludeArchived: in.IncludeArchived,
		})
		if err != nil {
			return nil, recallOut{}, err
		}
		out := recallOut{Memories: mems}
		if degraded {
			out.Degraded = "fts_only"
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "remember",
		Description: "Store a durable memory. Only knowledge that outlives this mission and helps an agent with no mission context. Not for task status or transient state.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in rememberIn) (*mcp.CallToolResult, memory.RememberResult, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, memory.RememberResult{}, err
		}
		store, err := s.memoryStore()
		if err != nil {
			return nil, memory.RememberResult{}, err
		}
		defer store.Close()

		scope := in.Scope
		if scope == "" {
			scope = "global"
			for repo := range m.Repos {
				scope = "repo:" + repo
				break
			}
		}
		res, err := store.Remember(ctx, memory.RememberInput{
			Content: in.Content, Kind: in.Kind, Scope: scope, Title: in.Title,
			Tags: in.Tags, Supersedes: in.Supersedes,
			Source: fmt.Sprintf("mission:%s/%s", s.missionID, s.actor()),
		})
		if err != nil {
			return nil, memory.RememberResult{}, err
		}
		return nil, *res, nil
	})
}

// ---- orchestrator tools ----

type missionStatusIn struct {
	Verbose bool `json:"verbose,omitempty"`
}

type agentSummary struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Persona  string `json:"persona,omitempty"`
	Model    string `json:"model,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Finished bool   `json:"finished"`
}

type missionStatusOut struct {
	ID           string             `json:"id"`
	Goal         string             `json:"goal"`
	Type         string             `json:"type"`
	Phase        string             `json:"phase"`
	YokeSeq      int                `json:"yoke_seq,omitempty"`
	Repos        []string           `json:"repos,omitempty"`
	SpentUSD     float64            `json:"spent_usd"`
	BudgetUSD    float64            `json:"budget_usd"`
	SpendFrozen  bool               `json:"spend_frozen,omitempty"`
	Agents       []agentSummary     `json:"agents,omitempty"`
	RecentEvents []string           `json:"recent_events,omitempty"`
	Checkpoint   mission.Checkpoint `json:"checkpoint,omitempty"`
}

type updateMissionIn struct {
	Phase             string  `json:"phase,omitempty" jsonschema:"transition to this phase (playbook-defined; 'closed' ends the mission)"`
	Goal              string  `json:"goal,omitempty"`
	Outcome           string  `json:"outcome,omitempty" jsonschema:"required when closing"`
	BudgetUSD         float64 `json:"budget_usd,omitempty" jsonschema:"raise/lower the mission budget"`
	MaxParallelAgents int     `json:"max_parallel_agents,omitempty"`
}
type updateMissionOut struct {
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
}

func (s *Server) registerOrchestrator(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "mission_status",
		Description: "Current mission state: phase, budget/spend, worker roster, recent events. Prefer this over re-reading files.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in missionStatusIn) (*mcp.CallToolResult, missionStatusOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, missionStatusOut{}, err
		}
		watcher.EnsureRunning(s.cfg, m)

		out := missionStatusOut{
			ID: m.ID, Goal: m.Goal, Type: m.Type, Phase: m.Phase,
			SpentUSD: m.SpentUSD, BudgetUSD: m.Budgets.MissionUSD,
			SpendFrozen: m.SpendFrozen, Checkpoint: m.Checkpoint,
		}
		if m.Yoke != nil {
			out.YokeSeq = m.Yoke.Seq
		}
		for repo := range m.Repos {
			out.Repos = append(out.Repos, repo)
		}
		out.Agents = s.agentSummaries(m)

		n := 8
		if in.Verbose {
			n = 30
		}
		if events, err := m.EventsSince(0); err == nil {
			start := max(0, len(events)-n)
			for _, ev := range events[start:] {
				line := fmt.Sprintf("[%s] %s (%s)", ev.TS.Format("15:04"), ev.Type, ev.Actor)
				out.RecentEvents = append(out.RecentEvents, line)
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_mission",
		Description: "Update mission state: phase transitions, goal, budget raises. Closing requires outcome and no running workers.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in updateMissionIn) (*mcp.CallToolResult, updateMissionOut, error) {
		var msg string
		m, err := mission.Update(s.oxHome, s.missionID, func(m *mission.Mission) error {
			if in.Goal != "" {
				m.Goal = in.Goal
			}
			if in.BudgetUSD > 0 {
				m.Budgets.MissionUSD = in.BudgetUSD
				if m.SpendFrozen && m.SpentUSD < in.BudgetUSD {
					m.SpendFrozen = false
					msg = "spend unfrozen"
				}
			}
			if in.MaxParallelAgents > 0 {
				m.Approvals.MaxParallelAgents = in.MaxParallelAgents
			}
			if in.Phase != "" && in.Phase != m.Phase {
				if in.Phase == mission.PhaseClosed {
					if running := s.runningAgents(m); len(running) > 0 {
						return fmt.Errorf("cannot close: %d workers still running (%s) — kill or wait first",
							len(running), strings.Join(running, ", "))
					}
					if in.Outcome == "" && m.Outcome == "" {
						return fmt.Errorf("closing requires an outcome")
					}
				}
				m.SetPhase(in.Phase, s.actor())
			}
			if in.Outcome != "" {
				m.Outcome = in.Outcome
			}
			return nil
		})
		if err != nil {
			return nil, updateMissionOut{}, err
		}
		if in.Phase != "" {
			m.AppendEvent("phase_changed", s.actor(), map[string]any{"phase": in.Phase})
		}
		return nil, updateMissionOut{Phase: m.Phase, Message: msg}, nil
	})
}

func (s *Server) registerWorker(srv *mcp.Server) {
	s.registerWorkerTools(srv)
}

func (s *Server) agentSummaries(m *mission.Mission) []agentSummary {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return nil
	}
	var out []agentSummary
	for _, w := range reg.Workers {
		out = append(out, agentSummary{
			ID: w.ID, Status: w.Status, Persona: w.Persona, Model: w.Model,
			Repo: w.Repo, Detail: w.Summary, Finished: w.Finished(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Server) runningAgents(m *mission.Mission) []string {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return nil
	}
	var running []string
	for _, w := range reg.Workers {
		if w.Status == harness.WorkerRunning || w.Status == harness.WorkerBlocked {
			running = append(running, w.ID)
		}
	}
	sort.Strings(running)
	return running
}

func appendDecision(dir, actor, content string) {
	path := dir + "/decisions.md"
	f, err := openAppend(path)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "## %s — %s\n%s\n\n", time.Now().Format("2006-01-02 15:04"), actor, content)
}
