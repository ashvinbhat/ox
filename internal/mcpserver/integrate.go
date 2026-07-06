package mcpserver

import (
	"context"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ashvinbhat/ox/internal/harness"
)

type mergeAgentsIn struct {
	Only    []string `json:"only,omitempty" jsonschema:"merge only these workers"`
	Skip    []string `json:"skip,omitempty" jsonschema:"skip these workers"`
	Confirm bool     `json:"confirm" jsonschema:"must be true — merging rewrites the integration branch"`
}
type mergeAgentsOut struct {
	Results []harness.MergeResult `json:"results"`
}

type shipIn struct {
	Repos   []string `json:"repos,omitempty" jsonschema:"default: all bound repos"`
	Draft   bool     `json:"draft,omitempty"`
	Title   string   `json:"title,omitempty" jsonschema:"PR title — describe the change, never the tooling"`
	Body    string   `json:"body,omitempty" jsonschema:"PR body markdown — same rule"`
	Confirm bool     `json:"confirm" jsonschema:"must be true — this pushes and opens PRs. Requires explicit user approval first."`
}
type shipOut struct {
	Results []harness.ShipResult `json:"results"`
}

type linkPRIn struct {
	Repo string `json:"repo"`
	URL  string `json:"url"`
}

func (s *Server) registerIntegrateTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "merge_agents",
		Description: "Merge done workers' branches into the integration branch in dependency order, build-gated per repo. Stops a repo's pipeline on conflict or build failure.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in mergeAgentsIn) (*mcp.CallToolResult, mergeAgentsOut, error) {
		if !in.Confirm {
			return nil, mergeAgentsOut{}, fmt.Errorf("set confirm=true to merge")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, mergeAgentsOut{}, err
		}
		results, err := harness.MergeWorkers(s.cfg, m, in.Only, in.Skip)
		if err != nil {
			return nil, mergeAgentsOut{}, err
		}
		return nil, mergeAgentsOut{Results: results}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ship",
		Description: "Push integration branches and open PRs. HARD GATE: only after the user explicitly approved shipping, and only in a late phase.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in shipIn) (*mcp.CallToolResult, shipOut, error) {
		if !in.Confirm {
			return nil, shipOut{}, fmt.Errorf("set confirm=true after the user approves shipping")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, shipOut{}, err
		}
		if !slices.Contains([]string{"reviewing", "shipping"}, m.Phase) {
			return nil, shipOut{}, fmt.Errorf("ship is phase-gated: current phase %q — move to reviewing/shipping first (update_mission)", m.Phase)
		}
		results := harness.Ship(s.cfg, m, in.Repos, in.Draft, in.Title, in.Body)
		return nil, shipOut{Results: results}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "link_pr",
		Description: "Record a PR created outside ship (gh, web UI) on the mission and its task.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in linkPRIn) (*mcp.CallToolResult, emptyOut, error) {
		if in.URL == "" {
			return nil, emptyOut{}, fmt.Errorf("url required")
		}
		m, err := s.openMission()
		if err != nil {
			return nil, emptyOut{}, err
		}
		harness.LinkPR(m, in.Repo, in.URL)
		return nil, emptyOut{OK: true}, nil
	})
}
