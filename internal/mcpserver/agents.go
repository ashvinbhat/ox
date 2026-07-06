package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/scratchpad"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

// ---- worker tools ----

type reportDoneIn struct {
	Summary string   `json:"summary" jsonschema:"what you did, files touched, how to verify, open items"`
	Outputs []string `json:"outputs,omitempty" jsonschema:"paths of artifacts produced"`
	Learned string   `json:"learned,omitempty" jsonschema:"one durable insight worth remembering, if any"`
}
type reportDoneOut struct {
	OK bool `json:"ok"`
}

type reportBlockerIn struct {
	Question string `json:"question" jsonschema:"the precise question blocking you"`
	Context  string `json:"context,omitempty" jsonschema:"what the answerer needs to know"`
}

func (s *Server) registerWorkerTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "report_done",
		Description: "Finish your subtask: records your summary and marks you done. Commit first. After this, run /exit.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in reportDoneIn) (*mcp.CallToolResult, reportDoneOut, error) {
		if strings.TrimSpace(in.Summary) == "" {
			return nil, reportDoneOut{}, fmt.Errorf("summary required")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, reportDoneOut{}, err
		}

		var out strings.Builder
		out.WriteString(in.Summary)
		if len(in.Outputs) > 0 {
			out.WriteString("\n\n## Outputs\n")
			for _, p := range in.Outputs {
				fmt.Fprintf(&out, "- %s\n", p)
			}
		}
		outputPath := m.Dir() + "/workers/" + s.agentID + "/output.md"
		if err := os.WriteFile(outputPath, []byte(out.String()), 0o644); err != nil {
			return nil, reportDoneOut{}, err
		}

		if err := harness.MarkWorkerFinished(s.oxHome, m, s.agentID, harness.WorkerDone, firstLine(in.Summary)); err != nil {
			return nil, reportDoneOut{}, err
		}
		m.AppendEvent("agent_done", s.actor(), map[string]any{
			"id": s.agentID, "summary": firstLine(in.Summary), "output": "workers/" + s.agentID + "/output.md",
		})

		if in.Learned != "" {
			if store, err := s.memoryStore(); err == nil {
				defer store.Close()
				w, _ := findRegWorker(m, s.agentID)
				scope := "global"
				if w != nil && w.Repo != "" {
					scope = "repo:" + w.Repo
				}
				store.Remember(ctx, memory.RememberInput{
					Content: in.Learned, Kind: "learning", Scope: scope,
					Source: fmt.Sprintf("mission:%s/%s", s.missionID, s.actor()),
				})
			}
		}
		return nil, reportDoneOut{OK: true}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "report_blocker",
		Description: "You are stuck: raises the question to the orchestrator with priority. Keep working on unblocked parts afterwards; the answer arrives here as a message.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in reportBlockerIn) (*mcp.CallToolResult, reportDoneOut, error) {
		if strings.TrimSpace(in.Question) == "" {
			return nil, reportDoneOut{}, fmt.Errorf("question required")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, reportDoneOut{}, err
		}

		content := in.Question
		if in.Context != "" {
			content += "\n\nContext: " + in.Context
		}
		scratchpad.New(m.Dir()).Append(scratchpad.Entry{AgentID: s.actor(), Kind: "blocker", Content: content})

		harness.UpdateRegistry(s.oxHome, m, func(reg *harness.Registry) error {
			if w := reg.Workers[s.agentID]; w != nil && w.Status == harness.WorkerRunning {
				w.Status = harness.WorkerBlocked
			}
			return nil
		})
		m.AppendEvent("agent_blocker", s.actor(), map[string]any{"id": s.agentID, "question": firstLine(in.Question)})
		return nil, reportDoneOut{OK: true}, nil
	})
}

// ---- orchestrator agent tools ----

type spawnAgentIn struct {
	ID           string   `json:"id" jsonschema:"kebab-case worker id, e.g. auth-api"`
	Repo         string   `json:"repo" jsonschema:"registered repo the worker codes in"`
	Brief        string   `json:"brief" jsonschema:"full subtask brief: goal, owned files, integration contract, done criteria"`
	Persona      string   `json:"persona,omitempty" jsonschema:"default builder"`
	Model        string   `json:"model,omitempty" jsonschema:"haiku|sonnet|opus; default from config"`
	Files        []string `json:"files,omitempty" jsonschema:"file globs this worker owns exclusively"`
	DependsOn    []string `json:"depends_on,omitempty" jsonschema:"worker ids that must finish first (auto-spawns when they do)"`
	MaxTurns     int      `json:"max_turns,omitempty"`
	MaxBudgetUSD float64  `json:"max_budget_usd,omitempty"`
}
type spawnAgentOut struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	TmuxSession string `json:"tmux_session,omitempty"`
	Worktree    string `json:"worktree,omitempty"`
	Branch      string `json:"branch,omitempty"`
}

type listAgentsIn struct{}
type listAgentsOut struct {
	Agents []agentSummary `json:"agents"`
}

type agentRefIn struct {
	ID string `json:"id"`
}
type peekAgentIn struct {
	ID    string `json:"id"`
	Lines int    `json:"lines,omitempty" jsonschema:"default 50"`
}
type peekAgentOut struct {
	Status string `json:"status"`
	Pane   string `json:"pane"`
}
type messageAgentIn struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	Background bool   `json:"background,omitempty" jsonschema:"true adds context without consuming the worker's turn"`
}
type killAgentIn struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}
type respawnAgentIn struct {
	ID           string `json:"id"`
	ExtraContext string `json:"extra_context,omitempty" jsonschema:"appended to the restart note"`
}

func (s *Server) registerAgentTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "spawn_agent",
		Description: "Spawn a session worker in its own worktree + tmux session. Only when the delegation ladder justifies it: parallelism, isolation, or long-running implementation. State the reason in the brief.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in spawnAgentIn) (*mcp.CallToolResult, spawnAgentOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, spawnAgentOut{}, err
		}
		w, err := harness.SpawnWorker(s.cfg, m, harness.SpawnInput{
			ID: in.ID, Repo: in.Repo, Brief: in.Brief, Persona: in.Persona, Model: in.Model,
			Files: in.Files, DependsOn: in.DependsOn, MaxTurns: in.MaxTurns, MaxBudgetUSD: in.MaxBudgetUSD,
		})
		if err != nil {
			return nil, spawnAgentOut{}, err
		}
		return nil, spawnAgentOut{
			ID: w.ID, Status: w.Status, TmuxSession: w.TmuxSession, Worktree: w.WorktreePath, Branch: w.BranchName,
		}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_agents",
		Description: "List this mission's workers with status.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listAgentsIn) (*mcp.CallToolResult, listAgentsOut, error) {
		m, err := s.openMission()
		if err != nil {
			return nil, listAgentsOut{}, err
		}
		return nil, listAgentsOut{Agents: s.agentSummaries(m)}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "peek_agent",
		Description: "Capture a worker's recent terminal output. Use only on anomaly (silent, blocked, over budget) — output.md is the normal channel.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in peekAgentIn) (*mcp.CallToolResult, peekAgentOut, error) {
		m, w, err := s.findWorker(in.ID)
		if err != nil {
			return nil, peekAgentOut{}, err
		}
		_ = m
		lines := in.Lines
		if lines <= 0 {
			lines = 50
		}
		if !tmuxutil.HasSession(w.TmuxSession) {
			return nil, peekAgentOut{Status: w.Status, Pane: "(session not running)"}, nil
		}
		pane, err := tmuxutil.CapturePane(w.TmuxSession, lines)
		if err != nil {
			return nil, peekAgentOut{}, err
		}
		return nil, peekAgentOut{Status: w.Status, Pane: pane}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "message_agent",
		Description: "Send a message into a worker's session (answer to a blocker, course correction).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in messageAgentIn) (*mcp.CallToolResult, emptyOut, error) {
		m, w, err := s.findWorker(in.ID)
		if err != nil {
			return nil, emptyOut{}, err
		}
		if !tmuxutil.HasSession(w.TmuxSession) {
			return nil, emptyOut{}, fmt.Errorf("worker %q session is not running (status %s)", w.ID, w.Status)
		}
		text := in.Text
		if in.Background {
			text = "/btw " + text
		}
		if err := tmuxutil.SendKeys(w.TmuxSession, text); err != nil {
			return nil, emptyOut{}, err
		}
		// An answered blocker goes back to running.
		harness.UpdateRegistry(s.oxHome, m, func(reg *harness.Registry) error {
			if cur := reg.Workers[w.ID]; cur != nil && cur.Status == harness.WorkerBlocked && !in.Background {
				cur.Status = harness.WorkerRunning
			}
			return nil
		})
		return nil, emptyOut{OK: true}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "kill_agent",
		Description: "Terminate a worker session. Its worktree and commits survive; locks are released.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in killAgentIn) (*mcp.CallToolResult, emptyOut, error) {
		m, w, err := s.findWorker(in.ID)
		if err != nil {
			return nil, emptyOut{}, err
		}
		if err := harness.KillWorker(s.cfg, m, w, in.Reason); err != nil {
			return nil, emptyOut{}, err
		}
		return nil, emptyOut{OK: true}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "respawn_agent",
		Description: "Restart a dead/interrupted worker in its existing worktree. Resume-first: its previous conversation is restored when possible.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in respawnAgentIn) (*mcp.CallToolResult, spawnAgentOut, error) {
		m, w, err := s.findWorker(in.ID)
		if err != nil {
			return nil, spawnAgentOut{}, err
		}
		if err := harness.RespawnWorker(s.cfg, m, w, in.ExtraContext); err != nil {
			return nil, spawnAgentOut{}, err
		}
		return nil, spawnAgentOut{ID: w.ID, Status: w.Status, TmuxSession: w.TmuxSession, Worktree: w.WorktreePath}, nil
	})
}

func (s *Server) findWorker(id string) (*mission.Mission, *harness.Worker, error) {
	m, err := s.openMission()
	if err != nil {
		return nil, nil, err
	}
	w, err := findRegWorker(m, id)
	if err != nil {
		return nil, nil, err
	}
	return m, w, nil
}

func findRegWorker(m *mission.Mission, id string) (*harness.Worker, error) {
	reg, err := harness.LoadRegistry(m)
	if err != nil {
		return nil, err
	}
	w, ok := reg.Workers[id]
	if !ok {
		return nil, fmt.Errorf("worker %q not found in mission %s", id, m.ID)
	}
	return w, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}
