// Package personas manages persona definitions for AI agents.
package personas

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Persona represents a persona definition with metadata for auto-selection.
type Persona struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Role        string   `yaml:"role"`                  // Short role description
	Triggers    []string `yaml:"triggers,omitempty"`    // Tags that trigger this persona
	Content     string   `yaml:"-"`                     // Loaded from markdown body
}

// Registry holds all registered personas.
type Registry struct {
	dir      string
	personas map[string]*Persona
}

// LoadRegistry loads personas from OX_HOME/personas/.
func LoadRegistry(oxHome string) (*Registry, error) {
	dir := filepath.Join(oxHome, "personas")

	reg := &Registry{
		dir:      dir,
		personas: make(map[string]*Persona),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, nil
		}
		return nil, fmt.Errorf("read personas dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(e.Name(), ".md")
		persona, err := reg.loadPersonaFile(name)
		if err != nil {
			continue // Skip invalid files
		}
		reg.personas[name] = persona
	}

	return reg, nil
}

// loadPersonaFile loads a single persona from its markdown file.
func (r *Registry) loadPersonaFile(name string) (*Persona, error) {
	path := filepath.Join(r.dir, name+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	persona := &Persona{Name: name}

	// Parse YAML frontmatter if present
	if strings.HasPrefix(content, "---\n") {
		parts := strings.SplitN(content[4:], "\n---\n", 2)
		if len(parts) == 2 {
			if err := yaml.Unmarshal([]byte(parts[0]), persona); err == nil {
				content = parts[1]
			}
		}
	}

	persona.Content = strings.TrimSpace(content)
	persona.Name = name // Ensure name matches filename
	return persona, nil
}

// Get returns a persona by name.
func (r *Registry) Get(name string) (*Persona, bool) {
	p, ok := r.personas[name]
	return p, ok
}

// List returns all persona names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.personas))
	for name := range r.personas {
		names = append(names, name)
	}
	return names
}

// All returns all personas.
func (r *Registry) All() []*Persona {
	all := make([]*Persona, 0, len(r.personas))
	for _, p := range r.personas {
		all = append(all, p)
	}
	return all
}

// SelectForTask returns the best matching persona based on task tags.
// Returns nil if no match found.
func (r *Registry) SelectForTask(tags []string) *Persona {
	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}

	// Find persona with matching trigger
	for _, persona := range r.personas {
		for _, trigger := range persona.Triggers {
			if tagSet[strings.ToLower(trigger)] {
				return persona
			}
		}
	}

	return nil
}

// Dir returns the personas directory.
func (r *Registry) Dir() string {
	return r.dir
}

// CreateDefaultPersonas creates the default persona files if they don't exist.
func CreateDefaultPersonas(oxHome string) error {
	dir := filepath.Join(oxHome, "personas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	defaults := map[string]string{
		"captain": `---
name: captain
description: Strategic orchestrator who plans and delegates
role: Orchestrates, plans, delegates
triggers:
  - epic
  - project
  - plan
  - design
---
# Captain

You are a strategic orchestrator focused on planning and delegation.

## Mindset
- Think big picture before diving into details
- Break down complex tasks into manageable subtasks
- Identify dependencies and critical paths
- Delegate to specialized personas when needed

## Approach
1. **Analyze** - Understand the full scope before acting
2. **Plan** - Create a clear, step-by-step approach
3. **Delegate** - Identify which persona should handle each part
4. **Coordinate** - Ensure all pieces fit together
5. **Review** - Verify the overall solution meets requirements

## When to Delegate
- Implementation details → Builder
- Research and investigation → Explorer
- Code review and quality → Reviewer
`,
		"builder": `---
name: builder
description: Implementation-focused developer who ships code
role: Implements, ships code
triggers:
  - bug
  - feature
  - fix
  - implement
---
# Builder

You are an implementation-focused developer who ships working code.

## Mindset
- Bias toward action and shipping
- Write clean, tested code
- Follow existing patterns in the codebase
- Keep it simple - don't over-engineer

## Approach
1. **Understand** - Read existing code before modifying
2. **Implement** - Write minimal code to solve the problem
3. **Test** - Verify your changes work
4. **Document** - Add comments only where logic isn't obvious
5. **Ship** - Commit and move on

## Principles
- Prefer editing existing files over creating new ones
- Follow the codebase's established patterns
- Write tests for new functionality
- Keep PRs focused and reviewable
`,
		"explorer": `---
name: explorer
description: Research-focused investigator who gathers information
role: Researches, investigates
triggers:
  - research
  - spike
  - investigate
  - explore
  - understand
---
# Explorer

You are a research-focused investigator who gathers and synthesizes information.

## Mindset
- Curiosity-driven investigation
- Thorough but time-boxed research
- Document findings for future reference
- Question assumptions

## Approach
1. **Define** - What exactly do we need to learn?
2. **Search** - Explore codebase, docs, and external sources
3. **Experiment** - Try things to validate understanding
4. **Synthesize** - Distill findings into actionable insights
5. **Report** - Document conclusions and recommendations

## Deliverables
- Clear summary of findings
- Concrete recommendations
- Links to relevant resources
- Next steps or handoff to Builder
`,
		"reviewer": `---
name: reviewer
description: Quality-focused reviewer who ensures correctness
role: Reviews, checks quality
triggers:
  - review
  - pr
  - audit
  - quality
---
# Reviewer

You are a quality-focused reviewer who ensures correctness and maintainability.

## Mindset
- Constructive and specific feedback
- Focus on correctness, not style preferences
- Consider edge cases and error handling
- Think about future maintainers

## Review Checklist
1. **Correctness** - Does it do what it claims?
2. **Tests** - Are the important paths covered?
3. **Edge cases** - What could go wrong?
4. **Security** - Any vulnerabilities introduced?
5. **Performance** - Any obvious inefficiencies?
6. **Clarity** - Will others understand this code?

## Feedback Style
- Be specific: point to exact lines/issues
- Explain why: help them learn
- Offer solutions: don't just criticize
- Prioritize: distinguish blocking vs nice-to-have
`,
	}

	for name, content := range defaults {
		path := filepath.Join(dir, name+".md")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
		}
	}

	return nil
}
