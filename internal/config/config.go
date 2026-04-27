package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RepoConfig holds per-repo configuration.
type RepoConfig struct {
	URL          string   `yaml:"url"`
	BaseBranch   string   `yaml:"base_branch,omitempty"`
	CopyFiles    []string `yaml:"copy_files,omitempty"`    // Files/dirs to copy from repo to worktree
	PostSetup    string   `yaml:"post_setup,omitempty"`    // Command to run after worktree setup
	BuildCommand string   `yaml:"build_command,omitempty"` // Command to verify build (for merge gates)
}

// MultiConfig holds multi-agent orchestration settings.
type MultiConfig struct {
	DefaultModel      string  `yaml:"default_model,omitempty"`             // Default model for builders (e.g. "sonnet")
	CaptainModel      string  `yaml:"captain_model,omitempty"`             // Model for captain (e.g. "opus")
	MaxAgents         int     `yaml:"max_agents,omitempty"`                // Maximum concurrent agents
	MaxBudgetPerAgent float64 `yaml:"max_budget_per_agent_usd,omitempty"`  // Default budget cap per agent
	MaxTotalBudget    float64 `yaml:"max_total_budget_usd,omitempty"`      // Total budget cap
	DefaultMaxTurns   int     `yaml:"default_max_turns,omitempty"`         // Default max turns per agent
}

// Defaults holds default settings.
type Defaults struct {
	Persona string `yaml:"persona,omitempty"`
}

// Config represents the ox.yaml configuration file.
type Config struct {
	Agent            string                 `yaml:"agent,omitempty"`
	IDE              string                 `yaml:"ide,omitempty"`
	YokeHome         string                 `yaml:"yoke_home,omitempty"`
	Repos            map[string]*RepoConfig `yaml:"repos,omitempty"`
	Defaults         Defaults               `yaml:"defaults,omitempty"`
	DashboardPort    int                    `yaml:"dashboard_port,omitempty"`
	SlackWebhookURL  string                 `yaml:"slack_webhook_url,omitempty"`
	FeedbackPassword string                 `yaml:"feedback_password,omitempty"`
	Multi            MultiConfig            `yaml:"multi,omitempty"`
	Home             string                 `yaml:"-"` // resolved OX_HOME (not persisted)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Agent:    "claude",
		IDE:      "windsurf",
		Repos:    make(map[string]*RepoConfig),
		Defaults: Defaults{Persona: "builder"},
		Multi: MultiConfig{
			DefaultModel:      "sonnet",
			CaptainModel:      "opus",
			MaxAgents:         5,
			MaxBudgetPerAgent: 10.0,
			MaxTotalBudget:    80.0,
			DefaultMaxTurns:   100,
		},
	}
}

// ResolveHome determines the OX_HOME directory.
// Resolution order: OX_HOME env var → ~/.ox
func ResolveHome() (string, error) {
	if env := os.Getenv("OX_HOME"); env != "" {
		return env, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".ox"), nil
}

// ConfigPath returns the path to ox.yaml.
func ConfigPath(oxHome string) string {
	return filepath.Join(oxHome, "ox.yaml")
}

// Load loads the config from OX_HOME/ox.yaml.
func Load() (*Config, error) {
	oxHome, err := ResolveHome()
	if err != nil {
		return nil, err
	}

	cfgPath := ConfigPath(oxHome)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("ox not initialized (run 'ox init')")
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.Home = oxHome
	if cfg.Repos == nil {
		cfg.Repos = make(map[string]*RepoConfig)
	}

	return &cfg, nil
}

// Save writes the config to OX_HOME/ox.yaml.
func Save(cfg *Config) error {
	if cfg.Home == "" {
		return fmt.Errorf("config home not set")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	cfgPath := ConfigPath(cfg.Home)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// EnsureDirs creates the required directory structure in OX_HOME.
func EnsureDirs(oxHome string) error {
	dirs := []string{
		oxHome,
		filepath.Join(oxHome, "repos"),
		filepath.Join(oxHome, "tasks"),
		filepath.Join(oxHome, "worktrees"),
		filepath.Join(oxHome, "skills"),
		filepath.Join(oxHome, "personas"),
		filepath.Join(oxHome, "hooks"),
		filepath.Join(oxHome, "agents"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	return nil
}
