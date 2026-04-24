package agent

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/gitutil"
)

// MergeResult tracks the outcome of merging one agent's work.
type MergeResult struct {
	AgentID   string
	Status    string // "merged", "skipped", "conflict", "build_failed"
	Error     string
	FileStat  string // git diff --stat output
	Duration  time.Duration
}

// MergeReport is the full merge pipeline summary.
type MergeReport struct {
	TaskID      string
	TaskSeq     int
	Results     []MergeResult
	TotalMerged int
	TotalFailed int
	TotalSkipped int
}

// MergePipeline runs the sequential merge pipeline for a task.
// It merges each done agent's branch into the integration branch, running build gates between merges.
func MergePipeline(mgr *Manager, cfg *config.Config, taskID string, skipAgents map[string]bool) (*MergeReport, error) {
	reg, err := mgr.LoadRegistry(taskID)
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}

	// Load plan for dependency ordering
	planPath := mgr.AgentsDir(taskID) + "/plan.md"
	plan, planErr := ParsePlan(planPath)

	// Determine merge order: topological sort if plan exists, otherwise registry order
	var orderedAgents []*Agent
	if planErr == nil && plan != nil {
		orderedAgents = topologicalSort(reg.Agents, plan)
	} else {
		orderedAgents = reg.Agents
	}

	report := &MergeReport{
		TaskID:  taskID,
		TaskSeq: reg.TaskSeq,
	}

	for _, a := range orderedAgents {
		// Skip non-done agents
		if a.Status != StatusDone && a.Status != StatusKilled {
			// Check if agent is still running — it hasn't finished yet
			if a.Status == StatusRunning || a.Status == StatusPending {
				report.Results = append(report.Results, MergeResult{
					AgentID: a.ID,
					Status:  "skipped",
					Error:   fmt.Sprintf("agent still %s", a.Status),
				})
				report.TotalSkipped++
				continue
			}
		}

		// Check skip list
		if skipAgents[a.ID] {
			report.Results = append(report.Results, MergeResult{
				AgentID: a.ID,
				Status:  "skipped",
				Error:   "user skipped",
			})
			report.TotalSkipped++
			continue
		}

		// Skip agents with no branch (e.g., captain)
		if a.BranchName == "" || a.WorktreePath == "" {
			continue
		}

		start := time.Now()
		result := mergeAgent(cfg, a)
		result.Duration = time.Since(start)
		report.Results = append(report.Results, result)

		switch result.Status {
		case "merged":
			report.TotalMerged++
		case "conflict", "build_failed":
			report.TotalFailed++
			// Stop on first failure — don't merge more on top of a broken state
			return report, fmt.Errorf("merge stopped: %s failed (%s)", a.ID, result.Status)
		default:
			report.TotalSkipped++
		}
	}

	return report, nil
}

func mergeAgent(cfg *config.Config, a *Agent) MergeResult {
	result := MergeResult{AgentID: a.ID}

	// Find the integration worktree (the main task worktree)
	// Agent worktrees are at: ~/.ox/worktrees/<repo>/<taskseq>-<agentid>/
	// The integration branch should be checked out in the base repo
	// We need to find a worktree for the same repo that's the integration target

	// For now, merge into the repo's main worktree
	// The agent's worktree path tells us the repo path
	repoName := a.Repo
	rc, exists := cfg.Repos[repoName]
	if !exists {
		result.Status = "skipped"
		result.Error = fmt.Sprintf("repo %q not in config", repoName)
		return result
	}

	// Find repo base path from worktree path
	// Worktree: ~/.ox/worktrees/<repo>/<taskseq>-<agentid>/
	// Repo: ~/.ox/repos/<repo>/
	worktreePath := a.WorktreePath
	// We merge in the agent's own worktree's repo context
	// First, get the diff stat
	stat, _ := gitutil.DiffStat(worktreePath, "HEAD~1", "HEAD")
	result.FileStat = stat

	// The integration target: we merge agent branch into the base branch
	// Find the base repo path
	parts := strings.Split(worktreePath, "/worktrees/")
	if len(parts) < 2 {
		result.Status = "skipped"
		result.Error = "cannot determine repo path"
		return result
	}
	basePath := parts[0] + "/repos/" + repoName

	// Get base branch
	baseBranch := "main"
	if rc.BaseBranch != "" {
		baseBranch = rc.BaseBranch
	}

	// Create integration branch if it doesn't exist
	integrationBranch := fmt.Sprintf("ox/%d-integration", a.TaskSeq)
	// Try to checkout or create integration branch in base repo
	_ = gitutil.Run(basePath, "checkout", "-B", integrationBranch, "origin/"+baseBranch)

	// Merge agent's branch
	mergeMsg := fmt.Sprintf("Merge agent: %s — %s", a.ID, truncate(a.SubtaskDesc, 60))
	if err := gitutil.MergeNoFF(basePath, a.BranchName, mergeMsg); err != nil {
		// Check if it's a merge conflict
		if gitutil.HasUncommittedChanges(basePath) {
			gitutil.MergeAbort(basePath)
			result.Status = "conflict"
			result.Error = err.Error()
		} else {
			result.Status = "conflict"
			result.Error = err.Error()
		}
		return result
	}

	// Run build gate if configured
	if rc.BuildCommand != "" {
		cmd := exec.Command("sh", "-c", rc.BuildCommand)
		cmd.Dir = basePath
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Build failed — abort the merge
			// Reset to before the merge
			gitutil.Run(basePath, "reset", "--hard", "HEAD~1")
			result.Status = "build_failed"
			result.Error = fmt.Sprintf("build command failed: %s\n%s", err, lastLines(string(output), 10))
			return result
		}
	}

	result.Status = "merged"
	return result
}

// DisplayReport prints the merge report.
func DisplayReport(report *MergeReport) {
	fmt.Printf("\n📊 Merge Report — Task #%d\n\n", report.TaskSeq)

	for _, r := range report.Results {
		icon := "?"
		switch r.Status {
		case "merged":
			icon = "✓"
		case "skipped":
			icon = "○"
		case "conflict":
			icon = "✗"
		case "build_failed":
			icon = "✗"
		}

		line := fmt.Sprintf("  %s %-20s [%s]", icon, r.AgentID, r.Status)
		if r.Duration > 0 {
			line += fmt.Sprintf(" (%s)", r.Duration.Truncate(time.Millisecond))
		}
		fmt.Println(line)

		if r.Error != "" {
			fmt.Printf("    Error: %s\n", r.Error)
		}
		if r.FileStat != "" {
			for _, l := range strings.Split(r.FileStat, "\n") {
				if strings.TrimSpace(l) != "" {
					fmt.Printf("    %s\n", l)
				}
			}
		}
	}

	fmt.Printf("\nTotal: %d merged, %d failed, %d skipped\n",
		report.TotalMerged, report.TotalFailed, report.TotalSkipped)
}

// topologicalSort orders agents by dependencies (agents with no deps first).
func topologicalSort(agents []*Agent, plan *Plan) []*Agent {
	// Build dependency map from plan
	depMap := make(map[string][]string)
	for _, pa := range plan.Agents {
		depMap[pa.ID] = pa.DependsOn
	}

	// Map agent IDs to agent objects
	agentMap := make(map[string]*Agent)
	for _, a := range agents {
		agentMap[a.ID] = a
	}

	visited := make(map[string]bool)
	var sorted []*Agent
	var visit func(id string)

	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, dep := range depMap[id] {
			visit(dep)
		}
		if a, ok := agentMap[id]; ok {
			sorted = append(sorted, a)
		}
	}

	// Visit all agents from the plan order
	for _, pa := range plan.Agents {
		visit(pa.ID)
	}

	// Add any agents not in the plan
	for _, a := range agents {
		if !visited[a.ID] {
			sorted = append(sorted, a)
		}
	}

	return sorted
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
