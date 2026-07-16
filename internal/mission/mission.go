// Package mission owns the durable state of a harness mission: the mission
// directory, mission.yaml, and the append-only event log. Files are the
// source of truth — every process (CLI, MCP server, watcher) is stateless
// between operations and coordinates through this store under a file lock.
package mission

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type YokeRef struct {
	ID  string `yaml:"id"`
	Seq int    `yaml:"seq"`
}

type RepoBinding struct {
	IntegrationBranch   string `yaml:"integration_branch"`
	IntegrationWorktree string `yaml:"integration_worktree"`
}

type PhaseChange struct {
	Phase string    `yaml:"phase"`
	At    time.Time `yaml:"at"`
	By    string    `yaml:"by"`
}

type Orchestrator struct {
	SessionID string `yaml:"session_id"`
	Model     string `yaml:"model"`
	Tmux      string `yaml:"tmux"`
}

type Budgets struct {
	MissionUSD  float64 `yaml:"mission_usd"`
	PerAgentUSD float64 `yaml:"per_agent_usd"`
	PerJobUSD   float64 `yaml:"per_job_usd"`
	Warned70    bool    `yaml:"warned_70,omitempty"`
}

type Approvals struct {
	AutoMerge         bool `yaml:"auto_merge"`
	MaxParallelAgents int  `yaml:"max_parallel_agents"`
}

type PRLink struct {
	Repo     string    `yaml:"repo"`
	URL      string    `yaml:"url"`
	LinkedAt time.Time `yaml:"linked_at"`
}

type Checkpoint struct {
	At            time.Time `yaml:"at,omitempty"`
	Done          string    `yaml:"done,omitempty"`
	Next          string    `yaml:"next,omitempty"`
	OpenQuestions []string  `yaml:"open_questions,omitempty"`
}

type Mission struct {
	Version      int                     `yaml:"version"`
	ID           string                  `yaml:"id"`
	Slug         string                  `yaml:"slug"`
	Type         string                  `yaml:"type"`
	Goal         string                  `yaml:"goal"`
	Yoke         *YokeRef                `yaml:"yoke,omitempty"`
	Repos        map[string]*RepoBinding `yaml:"repos,omitempty"`
	Phase        string                  `yaml:"phase"`
	PhaseHistory []PhaseChange           `yaml:"phase_history,omitempty"`
	Orchestrator Orchestrator            `yaml:"orchestrator"`
	Budgets      Budgets                 `yaml:"budgets"`
	Approvals    Approvals               `yaml:"approvals"`
	SpendFrozen  bool                    `yaml:"spend_frozen,omitempty"`
	SpentUSD     float64                 `yaml:"spent_usd"`
	PRs          []PRLink                `yaml:"prs,omitempty"`
	Checkpoint   Checkpoint              `yaml:"checkpoint,omitempty"`
	CreatedAt    time.Time               `yaml:"created_at"`
	UpdatedAt    time.Time               `yaml:"updated_at"`
	ClosedAt     *time.Time              `yaml:"closed_at,omitempty"`
	Outcome      string                  `yaml:"outcome,omitempty"`

	dir string
}

const (
	PhaseGathering = "gathering"
	PhaseClosed    = "closed"
)

func Root(oxHome string) string { return filepath.Join(oxHome, "missions") }

func (m *Mission) Dir() string      { return m.dir }
func (m *Mission) yamlPath() string { return filepath.Join(m.dir, "mission.yaml") }
func (m *Mission) TmuxSession() string {
	if m.Orchestrator.Tmux != "" {
		return m.Orchestrator.Tmux
	}
	return "ox-" + m.ID
}
func (m *Mission) Open() bool { return m.ClosedAt == nil }

// Create allocates the next mission ID, builds the directory skeleton, and
// persists mission.yaml.
func Create(oxHome, typ, goal string, yoke *YokeRef, model, sessionID string) (*Mission, error) {
	root := Root(oxHome)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create missions root: %w", err)
	}

	// Task-backed missions adopt the task's number (task #139 → m139) so the
	// two ids never diverge; ad-hoc missions draw from the counter, skipping
	// ids a task already claimed. Reopened tasks get a lettered suffix.
	var id string
	if yoke != nil && yoke.Seq > 0 {
		id = fmt.Sprintf("m%d", yoke.Seq)
		for suffix := 'b'; idTaken(root, id); suffix++ {
			id = fmt.Sprintf("m%d%c", yoke.Seq, suffix)
		}
	} else {
		for {
			seq, err := nextSeq(root)
			if err != nil {
				return nil, err
			}
			id = fmt.Sprintf("m%d", seq)
			if !idTaken(root, id) {
				break
			}
		}
	}
	slug := slugify(goal)
	dir := filepath.Join(root, id+"-"+slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create mission dir: %w", err)
	}
	for _, sub := range []string{"workers", "jobs", ".checkpoints"} {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}

	now := time.Now()
	m := &Mission{
		Version: 1,
		ID:      id,
		Slug:    slug,
		Type:    typ,
		Goal:    goal,
		Yoke:    yoke,
		Phase:   PhaseGathering,
		PhaseHistory: []PhaseChange{
			{Phase: PhaseGathering, At: now, By: "system"},
		},
		Orchestrator: Orchestrator{
			SessionID: sessionID,
			Model:     model,
			Tmux:      "ox-" + id,
		},
		Budgets:   Budgets{MissionUSD: 15, PerAgentUSD: 8, PerJobUSD: 2},
		Approvals: Approvals{MaxParallelAgents: 4},
		CreatedAt: now,
		UpdatedAt: now,
		dir:       dir,
	}

	if err := m.Save(); err != nil {
		return nil, err
	}
	m.AppendEvent("mission_created", "system", map[string]any{"type": typ, "goal": goal})
	return m, nil
}

// Save writes mission.yaml atomically (temp file + rename).
func (m *Mission) Save() error {
	m.UpdatedAt = time.Now()
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal mission: %w", err)
	}
	tmp := m.yamlPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write mission: %w", err)
	}
	return os.Rename(tmp, m.yamlPath())
}

// Update reloads the mission under the mission lock, applies fn, and saves.
// Use for read-modify-write from concurrent processes (MCP server, watcher).
func Update(oxHome, id string, fn func(*Mission) error) (*Mission, error) {
	var out *Mission
	err := WithLock(oxHome, id, func() error {
		m, err := Open(oxHome, id)
		if err != nil {
			return err
		}
		if err := fn(m); err != nil {
			return err
		}
		out = m
		return m.Save()
	})
	return out, err
}

// SetPhase records a phase transition.
func (m *Mission) SetPhase(phase, by string) {
	m.Phase = phase
	m.PhaseHistory = append(m.PhaseHistory, PhaseChange{Phase: phase, At: time.Now(), By: by})
	if phase == PhaseClosed {
		now := time.Now()
		m.ClosedAt = &now
	}
}

// Open loads a mission by ID (e.g. "m17").
func Open(oxHome, id string) (*Mission, error) {
	dir, err := findDir(oxHome, id)
	if err != nil {
		return nil, err
	}
	return load(dir)
}

// FindByYokeSeq returns the open mission linked to a yoke task, if any.
func FindByYokeSeq(oxHome string, seq int) (*Mission, error) {
	missions, err := List(oxHome)
	if err != nil {
		return nil, err
	}
	for _, m := range missions {
		if m.Open() && m.Yoke != nil && m.Yoke.Seq == seq {
			return m, nil
		}
	}
	return nil, os.ErrNotExist
}

// List loads every mission (open and closed), newest first.
func List(oxHome string) ([]*Mission, error) {
	root := Root(oxHome)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read missions root: %w", err)
	}

	var missions []*Mission
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "m") {
			continue
		}
		m, err := load(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		missions = append(missions, m)
	}
	// Newest first by numeric seq
	for i := 0; i < len(missions); i++ {
		for j := i + 1; j < len(missions); j++ {
			if seqOf(missions[j].ID) > seqOf(missions[i].ID) {
				missions[i], missions[j] = missions[j], missions[i]
			}
		}
	}
	return missions, nil
}

func load(dir string) (*Mission, error) {
	data, err := os.ReadFile(filepath.Join(dir, "mission.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read mission.yaml: %w", err)
	}
	var m Mission
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse mission.yaml: %w", err)
	}
	m.dir = dir
	return &m, nil
}

func findDir(oxHome, id string) (string, error) {
	root := Root(oxHome)
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("no missions found: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && (e.Name() == id || strings.HasPrefix(e.Name(), id+"-")) {
			return filepath.Join(root, e.Name()), nil
		}
	}
	return "", fmt.Errorf("mission %s not found", id)
}

func seqOf(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "m"))
	return n
}

// idTaken reports whether a mission directory already claims this id — the
// glob is anchored with "-" so m13 doesn't match m139's dir.
func idTaken(root, id string) bool {
	matches, _ := filepath.Glob(filepath.Join(root, id+"-*"))
	return len(matches) > 0
}

// nextSeq increments the counter file under the root lock so concurrent
// creates can't collide.
func nextSeq(root string) (int, error) {
	counterPath := filepath.Join(root, ".counter")
	var seq int
	err := withFileLock(filepath.Join(root, ".counter.lock"), func() error {
		data, err := os.ReadFile(counterPath)
		if err == nil {
			seq, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		seq++
		return os.WriteFile(counterPath, []byte(strconv.Itoa(seq)), 0o644)
	})
	return seq, err
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	slug := slugRe.ReplaceAllString(strings.ToLower(s), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		slug = "mission"
	}
	return slug
}
