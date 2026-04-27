package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ReviewerRole defines a reviewer's perspective.
type ReviewerRole struct {
	ID     string
	Name   string
	Prompt string
}

// ReviewResult holds one reviewer's output.
type ReviewResult struct {
	Role    ReviewerRole
	Content string
	Error   error
}

// DefaultReviewers returns the standard review panel.
func DefaultReviewers() []ReviewerRole {
	return []ReviewerRole{
		{
			ID:   "architect",
			Name: "Architect",
			Prompt: `You are reviewing a multi-agent execution plan as a SOFTWARE ARCHITECT. Be thorough and critical.

Review the plan for:
- Is the task decomposition correct? Are the boundaries between agents clean?
- Are dependencies modeled correctly? Could anything run in parallel that's listed as sequential, or vice versa?
- Are file ownership patterns comprehensive? Are there shared files (configs, imports, interfaces) that multiple agents might need to modify?
- Is there a missing agent? (e.g., no one handles database migrations, or shared types)
- Would the pieces actually fit together when merged?

For each issue found, rate it: [CRITICAL] [WARNING] [INFO]
Suggest specific fixes.

If the plan looks solid, say so — but still note any minor improvements.`,
		},
		{
			ID:   "guardian",
			Name: "Guardian",
			Prompt: `You are reviewing a multi-agent execution plan as a GUARDIAN focused on safety and backward compatibility. Be strict and adversarial.

Review the plan for:
- Backward compatibility: Will existing APIs, database schemas, or interfaces break?
- Data migrations: Are there schema changes that need migration scripts? Is there a rollback plan?
- Security implications: Authentication, authorization, input validation, secret handling
- Error handling: What happens if one agent's work fails at runtime? Are there cascading failures?
- Race conditions: Could agents create conflicting state if their work runs concurrently?
- Missing validation: Are there edge cases or boundary conditions not covered?

For each issue found, rate it: [CRITICAL] [WARNING] [INFO]
Be specific about what could go wrong and how to prevent it.`,
		},
		{
			ID:   "pragmatist",
			Name: "Pragmatist",
			Prompt: `You are reviewing a multi-agent execution plan as a PRAGMATIST focused on simplicity and efficiency. Push back on over-engineering.

Review the plan for:
- Over-engineering: Can any agents be consolidated? Is the decomposition too fine-grained?
- Model selection: Are expensive models (opus) assigned where cheaper ones (haiku) would suffice?
- Cost: Is the number of agents proportionate to the task complexity?
- Missing simplifications: Could the task be done with fewer agents? With 1 agent?
- Scope creep: Are agents doing more than the original task requires?
- Test strategy: Is the testing approach appropriate (too much, too little)?

For each issue found, rate it: [CRITICAL] [WARNING] [INFO]
Be direct. If the plan is over-complicated, say so.`,
		},
	}
}

// RunReviewPanel runs all reviewers in parallel against a plan.
func RunReviewPanel(agentsDir string, planPath string, repos []string, repoDirs map[string]string, oxHome string, model string) ([]ReviewResult, error) {
	planContent, err := os.ReadFile(planPath)
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}

	// Read AGENTS.md for task context
	agentsMdPath := filepath.Join(agentsDir, "AGENTS.md")
	agentsMd, _ := os.ReadFile(agentsMdPath)

	reviewers := DefaultReviewers()
	results := make([]ReviewResult, len(reviewers))
	var wg sync.WaitGroup

	for i, reviewer := range reviewers {
		wg.Add(1)
		go func(idx int, r ReviewerRole) {
			defer wg.Done()

			prompt := fmt.Sprintf(`%s

## Task Context
%s

## Plan to Review
%s

Write your review. Be specific and actionable.`, r.Prompt, string(agentsMd), string(planContent))

			content, err := runClaudeReview(agentsDir, prompt, repos, repoDirs, oxHome, model)
			results[idx] = ReviewResult{
				Role:    r,
				Content: content,
				Error:   err,
			}
		}(i, reviewer)
	}

	wg.Wait()

	// Save reviews to disk
	for _, r := range results {
		if r.Error != nil {
			continue
		}
		reviewPath := filepath.Join(agentsDir, fmt.Sprintf("review_%s.md", r.Role.ID))
		os.WriteFile(reviewPath, []byte(r.Content), 0o644)
	}

	return results, nil
}

// RunCaptainRevision has the captain revise the plan based on reviews.
func RunCaptainRevision(agentsDir string, repos []string, repoDirs map[string]string, oxHome string, model string) error {
	planPath := filepath.Join(agentsDir, "plan.md")
	planContent, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	// Collect all reviews
	var reviewSection strings.Builder
	reviewers := DefaultReviewers()
	for _, r := range reviewers {
		reviewPath := filepath.Join(agentsDir, fmt.Sprintf("review_%s.md", r.ID))
		content, err := os.ReadFile(reviewPath)
		if err != nil {
			continue
		}
		reviewSection.WriteString(fmt.Sprintf("\n## %s Review\n%s\n", r.Name, string(content)))
	}

	// Read original AGENTS.md for context
	agentsMdPath := filepath.Join(agentsDir, "AGENTS.md")
	agentsMd, _ := os.ReadFile(agentsMdPath)

	prompt := fmt.Sprintf(`You are the CAPTAIN revising your plan based on reviewer feedback.

## Original Task
%s

## Your Original Plan
%s

## Reviewer Feedback
%s

## Instructions
1. Read each reviewer's feedback carefully
2. For each issue raised, decide: ACCEPT (fix it) or REJECT (explain why)
3. Write the REVISED plan to %s — use the same format as the original
4. Write a decision log to %s explaining each accept/reject decision

Decision log format:
# Decision Log

## Accepted
- [reviewer] issue description → what you changed

## Rejected
- [reviewer] issue description → why you rejected it

Be concise. Only change what the reviews justify changing.`,
		string(agentsMd),
		string(planContent),
		reviewSection.String(),
		planPath,
		filepath.Join(agentsDir, "decisions.md"),
	)

	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--verbose",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--max-turns", "20")

	for _, repo := range repos {
		dir, ok := repoDirs[repo]
		if !ok {
			dir = filepath.Join(oxHome, "repos", repo)
		}
		args = append(args, "--add-dir", dir)
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = agentsDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// DisplayReviewSummary shows a compact summary of review results.
func DisplayReviewSummary(results []ReviewResult) {
	fmt.Println("\n📝 Review Summary:")
	for _, r := range results {
		if r.Error != nil {
			fmt.Printf("  ✗ %-12s error: %v\n", r.Role.Name+":", r.Error)
			continue
		}

		// Count issue severities
		critical := strings.Count(r.Content, "[CRITICAL]")
		warning := strings.Count(r.Content, "[WARNING]")
		info := strings.Count(r.Content, "[INFO]")

		status := "✓"
		if critical > 0 {
			status = "✗"
		} else if warning > 0 {
			status = "⚠"
		}

		summary := extractFirstSentence(r.Content)
		if len(summary) > 80 {
			summary = summary[:77] + "..."
		}

		fmt.Printf("  %s %-12s", status, r.Role.Name+":")
		if critical+warning+info > 0 {
			parts := []string{}
			if critical > 0 {
				parts = append(parts, fmt.Sprintf("%d critical", critical))
			}
			if warning > 0 {
				parts = append(parts, fmt.Sprintf("%d warning", warning))
			}
			if info > 0 {
				parts = append(parts, fmt.Sprintf("%d info", info))
			}
			fmt.Printf(" %s", strings.Join(parts, ", "))
		}
		fmt.Printf("\n    %s\n", summary)
	}
}

func runClaudeReview(dir, prompt string, repos []string, repoDirs map[string]string, oxHome, model string) (string, error) {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "text",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--max-turns", "10")

	for _, repo := range repos {
		repoDir, ok := repoDirs[repo]
		if !ok {
			repoDir = filepath.Join(oxHome, "repos", repo)
		}
		args = append(args, "--add-dir", repoDir)
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude review: %w", err)
	}

	return string(output), nil
}

func extractFirstSentence(s string) string {
	s = strings.TrimSpace(s)
	// Skip markdown headers
	for strings.HasPrefix(s, "#") {
		idx := strings.Index(s, "\n")
		if idx < 0 {
			return s
		}
		s = strings.TrimSpace(s[idx+1:])
	}
	// Find first sentence
	for _, end := range []string{". ", ".\n", "!"} {
		if idx := strings.Index(s, end); idx > 0 && idx < 200 {
			return s[:idx+1]
		}
	}
	if len(s) > 100 {
		return s[:100]
	}
	return s
}
