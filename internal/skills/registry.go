// Package skills manages skill definitions and auto-injection.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a skill definition with metadata for auto-injection.
type Skill struct {
	Name        string   `yaml:"-"`              // Derived from key
	File        string   `yaml:"file"`           // Path to skill markdown file
	Description string   `yaml:"description"`    // Short description
	Tags        []string `yaml:"tags,omitempty"` // Tags that trigger this skill
	Personas    []string `yaml:"personas,omitempty"` // Personas that use this skill
	TaskTypes   []string `yaml:"task_types,omitempty"` // Task types (bug, feature, etc.)
}

// Registry holds all registered skills.
type Registry struct {
	Skills map[string]*Skill `yaml:"skills"`
	path   string            // Path to skills.yaml
	dir    string            // Skills directory
}

// LoadRegistry loads the skills registry from OX_HOME/skills/skills.yaml.
func LoadRegistry(oxHome string) (*Registry, error) {
	dir := filepath.Join(oxHome, "skills")
	path := filepath.Join(dir, "skills.yaml")

	reg := &Registry{
		Skills: make(map[string]*Skill),
		path:   path,
		dir:    dir,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No registry yet, return empty
			return reg, nil
		}
		return nil, fmt.Errorf("read skills.yaml: %w", err)
	}

	if err := yaml.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parse skills.yaml: %w", err)
	}

	// Set name from key
	for name, skill := range reg.Skills {
		skill.Name = name
	}

	return reg, nil
}

// Save writes the registry to skills.yaml.
func (r *Registry) Save() error {
	// Ensure directory exists
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal skills.yaml: %w", err)
	}

	if err := os.WriteFile(r.path, data, 0o644); err != nil {
		return fmt.Errorf("write skills.yaml: %w", err)
	}

	return nil
}

// Add registers a new skill.
func (r *Registry) Add(name string, skill *Skill) error {
	if _, exists := r.Skills[name]; exists {
		return fmt.Errorf("skill %q already exists", name)
	}

	skill.Name = name
	if skill.File == "" {
		skill.File = name + ".md"
	}

	r.Skills[name] = skill
	return r.Save()
}

// Remove unregisters a skill.
func (r *Registry) Remove(name string) error {
	if _, exists := r.Skills[name]; !exists {
		return fmt.Errorf("skill %q not found", name)
	}

	delete(r.Skills, name)
	return r.Save()
}

// Get returns a skill by name.
func (r *Registry) Get(name string) (*Skill, bool) {
	skill, ok := r.Skills[name]
	return skill, ok
}

// List returns all skill names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.Skills))
	for name := range r.Skills {
		names = append(names, name)
	}
	return names
}

// MatchForTask returns skills that match the given criteria.
// Skills are matched by tags first. Persona-only matching is only used for
// skills that have NO tags defined (generic skills that apply to all tasks).
func (r *Registry) MatchForTask(tags []string, persona string, taskType string) []*Skill {
	var matches []*Skill
	seen := make(map[string]bool)

	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}

	for _, skill := range r.Skills {
		if seen[skill.Name] {
			continue
		}

		// Check tag match - this is the primary filter
		tagMatched := false
		for _, st := range skill.Tags {
			if tagSet[strings.ToLower(st)] {
				matches = append(matches, skill)
				seen[skill.Name] = true
				tagMatched = true
				break
			}
		}
		if seen[skill.Name] {
			continue
		}

		// Check task type match
		for _, tt := range skill.TaskTypes {
			if strings.EqualFold(tt, taskType) {
				matches = append(matches, skill)
				seen[skill.Name] = true
				break
			}
		}
		if seen[skill.Name] {
			continue
		}

		// Persona-only matching: ONLY if skill has no tags defined
		// This prevents backend skills from being included in frontend tasks
		// just because they share the same persona
		if len(skill.Tags) == 0 && !tagMatched {
			for _, sp := range skill.Personas {
				if strings.EqualFold(sp, persona) {
					matches = append(matches, skill)
					seen[skill.Name] = true
					break
				}
			}
		}
	}

	return matches
}

// GetContent reads the skill file content.
func (r *Registry) GetContent(skill *Skill) (string, error) {
	path := filepath.Join(r.dir, skill.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read skill file %s: %w", skill.File, err)
	}
	return string(data), nil
}

// SkillPath returns the full path to a skill file.
func (r *Registry) SkillPath(skill *Skill) string {
	return filepath.Join(r.dir, skill.File)
}

// Dir returns the skills directory.
func (r *Registry) Dir() string {
	return r.dir
}
