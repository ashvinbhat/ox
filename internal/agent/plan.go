package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Plan represents a parsed captain plan.
type Plan struct {
	Title    string
	Overview string
	Agents   []PlannedAgent
}

// PlannedAgent represents an agent defined in the plan.
type PlannedAgent struct {
	ID          string
	Repo        string
	Persona     string
	Model       string
	Files       []string
	DependsOn   []string
	Description string
}

// ParsePlan parses a plan.md file into a structured Plan.
func ParsePlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}
	return ParsePlanContent(string(data))
}

// ParsePlanContent parses plan content string into a structured Plan.
func ParsePlanContent(content string) (*Plan, error) {
	plan := &Plan{}
	scanner := bufio.NewScanner(strings.NewReader(content))

	var section string       // current h2 section
	var agentID string       // current h3 agent
	var currentAgent *PlannedAgent
	var descLines []string
	var inDescription bool

	flushAgent := func() {
		if currentAgent != nil {
			if inDescription && len(descLines) > 0 {
				currentAgent.Description = strings.TrimSpace(strings.Join(descLines, "\n"))
			}
			plan.Agents = append(plan.Agents, *currentAgent)
		}
		currentAgent = nil
		descLines = nil
		inDescription = false
		agentID = ""
	}

	for scanner.Scan() {
		line := scanner.Text()

		// H1: Plan title
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			plan.Title = strings.TrimPrefix(line, "# ")
			plan.Title = strings.TrimPrefix(plan.Title, "Plan: ")
			continue
		}

		// H2: Section
		if strings.HasPrefix(line, "## ") {
			flushAgent()
			section = strings.TrimPrefix(line, "## ")
			continue
		}

		// H3: Agent definition
		if strings.HasPrefix(line, "### ") {
			flushAgent()
			agentID = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			currentAgent = &PlannedAgent{
				ID:      agentID,
				Persona: "builder", // default
				Model:   "sonnet",  // default
			}
			continue
		}

		// Parse section content
		switch strings.ToLower(section) {
		case "overview":
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				if plan.Overview != "" {
					plan.Overview += " "
				}
				plan.Overview += trimmed
			}

		case "agents":
			if currentAgent == nil {
				continue
			}

			trimmed := strings.TrimSpace(line)

			// Parse key-value fields
			if strings.HasPrefix(trimmed, "- repo:") {
				currentAgent.Repo = parseValue(trimmed, "- repo:")
				inDescription = false
			} else if strings.HasPrefix(trimmed, "- persona:") {
				currentAgent.Persona = parseValue(trimmed, "- persona:")
				inDescription = false
			} else if strings.HasPrefix(trimmed, "- model:") {
				currentAgent.Model = parseValue(trimmed, "- model:")
				inDescription = false
			} else if strings.HasPrefix(trimmed, "- files:") {
				currentAgent.Files = parseList(trimmed, "- files:")
				inDescription = false
			} else if strings.HasPrefix(trimmed, "- depends_on:") {
				currentAgent.DependsOn = parseList(trimmed, "- depends_on:")
				inDescription = false
			} else if strings.HasPrefix(trimmed, "- description:") {
				inDescription = true
				// Check for inline description
				val := parseValue(trimmed, "- description:")
				if val != "" && val != "|" {
					descLines = append(descLines, val)
					inDescription = false
				}
			} else if inDescription {
				descLines = append(descLines, line)
			}
		}
	}

	// Flush last agent
	flushAgent()

	if len(plan.Agents) == 0 {
		return nil, fmt.Errorf("no agents found in plan")
	}

	return plan, nil
}

// ValidatePlan checks a plan for common issues.
func ValidatePlan(plan *Plan, availableRepos []string) []string {
	var issues []string
	repoSet := make(map[string]bool)
	for _, r := range availableRepos {
		repoSet[r] = true
	}

	agentIDs := make(map[string]bool)
	for _, a := range plan.Agents {
		// Check for duplicate IDs
		if agentIDs[a.ID] {
			issues = append(issues, fmt.Sprintf("duplicate agent ID: %q", a.ID))
		}
		agentIDs[a.ID] = true

		// Check repo exists
		if a.Repo != "" && !repoSet[a.Repo] {
			issues = append(issues, fmt.Sprintf("agent %q uses unknown repo %q", a.ID, a.Repo))
		}

		// Check depends_on references exist
		for _, dep := range a.DependsOn {
			if !agentIDs[dep] {
				// Might be defined later, defer this check
				found := false
				for _, other := range plan.Agents {
					if other.ID == dep {
						found = true
						break
					}
				}
				if !found {
					issues = append(issues, fmt.Sprintf("agent %q depends on unknown agent %q", a.ID, dep))
				}
			}
		}

		// Check for empty description
		if a.Description == "" {
			issues = append(issues, fmt.Sprintf("agent %q has no description", a.ID))
		}
	}

	// Check for file ownership overlaps
	for i, a := range plan.Agents {
		for j, b := range plan.Agents {
			if i >= j {
				continue
			}
			for _, fa := range a.Files {
				for _, fb := range b.Files {
					if fa == fb {
						issues = append(issues, fmt.Sprintf("file conflict: agents %q and %q both claim %q", a.ID, b.ID, fa))
					}
				}
			}
		}
	}

	return issues
}

// DisplayPlan prints a human-readable plan summary.
func DisplayPlan(plan *Plan) {
	fmt.Printf("\n📋 Plan: %s\n", plan.Title)
	if plan.Overview != "" {
		fmt.Printf("\n%s\n", plan.Overview)
	}
	fmt.Printf("\n%d agent(s):\n\n", len(plan.Agents))

	for i, a := range plan.Agents {
		fmt.Printf("  %d. %s\n", i+1, a.ID)
		fmt.Printf("     Repo: %s | Persona: %s | Model: %s\n", a.Repo, a.Persona, a.Model)
		if len(a.Files) > 0 {
			fmt.Printf("     Files: %s\n", strings.Join(a.Files, ", "))
		}
		if len(a.DependsOn) > 0 {
			fmt.Printf("     Depends on: %s\n", strings.Join(a.DependsOn, ", "))
		}
		if a.Description != "" {
			// Truncate for display
			desc := a.Description
			if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			fmt.Printf("     %s\n", desc)
		}
		fmt.Println()
	}
}

func parseValue(line, prefix string) string {
	val := strings.TrimPrefix(line, prefix)
	return strings.TrimSpace(val)
}

func parseList(line, prefix string) []string {
	val := strings.TrimPrefix(line, prefix)
	val = strings.TrimSpace(val)
	// Handle [item1, item2] format
	val = strings.Trim(val, "[]")
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
