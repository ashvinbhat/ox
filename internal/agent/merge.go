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
	AgentID  string
	Status   string // "merged", "skipped", "conflict", "build_failed"
	Error    string
	FileStat string // git diff --stat output
	Duration time.Duration
}

// MergeReport is the full merge pipeline summary.
type MergeReport struct {
	TaskID            string
	TaskSeq           int
	IntegrationBranch string
	Results           []MergeResult
	TotalMerged       int
	TotalFailed       int
	TotalSkipped      int
}

// MergePipeline runs the sequential merge pipeline for a task.
// It merges each done agent's branch into the integration worktree, running build gates between merges.
func MergePipeline(mgr *Manager, cfg *config.Config, taskID string, skipAgents map[string]bool) (*MergeReport, error) {
	reg, err := mgr.LoadRegistry(taskID)
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}

	if len(reg.IntegrationBranches) == 0 {
		return nil, fmt.Errorf("no integration worktrees found — was this task started with 'ox multi'?")
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
		TaskID:            taskID,
		TaskSeq:           reg.TaskSeq,
		IntegrationBranch: reg.IntegrationBranch,
	}

	for _, a := range orderedAgents {
		// Skip non-done agents
		if a.Status != StatusDone && a.Status != StatusKilled {
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

		// Find integration worktree for this agent's repo
		integrationPath, exists := reg.IntegrationBranches[a.Repo]
		if !exists {
			report.Results = append(report.Results, MergeResult{
				AgentID: a.ID,
				Status:  "skipped",
				Error:   fmt.Sprintf("no integration worktree for repo %q", a.Repo),
			})
			report.TotalSkipped++
			continue
		}

		start := time.Now()
		result := mergeAgentInto(cfg, a, integrationPath)
		result.Duration = time.Since(start)
		report.Results = append(report.Results, result)

		switch result.Status {
		case "merged":
			report.TotalMerged++
		case "conflict", "build_failed":
			report.TotalFailed++
			return report, fmt.Errorf("merge stopped: %s failed (%s)", a.ID, result.Status)
		default:
			report.TotalSkipped++
		}
	}

	return report, nil
}

func mergeAgentInto(cfg *config.Config, a *Agent, integrationPath string) MergeResult {
	result := MergeResult{AgentID: a.ID}

	rc := cfg.Repos[a.Repo]

	// Get diff stat from agent's branch
	stat, _ := gitutil.DiffStat(a.WorktreePath, "HEAD~1", "HEAD")
	result.FileStat = stat

	// Merge agent's branch into the integration worktree
	mergeMsg := fmt.Sprintf("Merge agent: %s — %s", a.ID, truncate(a.SubtaskDesc, 60))
	if err := gitutil.MergeNoFF(integrationPath, a.BranchName, mergeMsg); err != nil {
		if gitutil.HasUncommittedChanges(integrationPath) {
			gitutil.MergeAbort(integrationPath)
		}
		result.Status = "conflict"
		result.Error = err.Error()
		return result
	}

	// Run build gate if configured
	if rc != nil && rc.BuildCommand != "" {
		cmd := exec.Command("sh", "-c", rc.BuildCommand)
		cmd.Dir = integrationPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Build failed — undo the merge
			gitutil.Run(integrationPath, "reset", "--hard", "HEAD~1")
			result.Status = "build_failed"
			result.Error = fmt.Sprintf("build failed: %s\n%s", err, lastLines(string(output), 10))
			return result
		}
	}

	result.Status = "merged"
	return result
}

// DisplayReport prints the merge report.
func DisplayReport(report *MergeReport) {
	fmt.Printf("\n📊 Merge Report — Task #%d\n", report.TaskSeq)
	if report.IntegrationBranch != "" {
		fmt.Printf("   Branch: %s\n", report.IntegrationBranch)
	}
	fmt.Println()

	for _, r := range report.Results {
		icon := "?"
		switch r.Status {
		case "merged":
			icon = "✓"
		case "skipped":
			icon = "○"
		case "conflict", "build_failed":
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

	if report.IntegrationBranch != "" && report.TotalMerged > 0 {
		fmt.Printf("\nAll merged into branch: %s\n", report.IntegrationBranch)
		fmt.Println("Push this branch and create a PR to main.")
	}
}

// topologicalSort orders agents by dependencies (agents with no deps first).
func topologicalSort(agents []*Agent, plan *Plan) []*Agent {
	depMap := make(map[string][]string)
	for _, pa := range plan.Agents {
		depMap[pa.ID] = pa.DependsOn
	}

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

	for _, pa := range plan.Agents {
		visit(pa.ID)
	}

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
