package harness

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

type ShipResult struct {
	Repo  string `json:"repo"`
	PRURL string `json:"pr_url,omitempty"`
	Error string `json:"error,omitempty"`
}

// Ship pushes each bound repo's integration branch and opens a PR. Titles and
// bodies describe the change only — nothing about the tooling leaks out.
func Ship(cfg *config.Config, m *mission.Mission, repos []string, draft bool, title, body string) []ShipResult {
	if title == "" {
		title = m.Goal
	}
	if body == "" {
		body = fmt.Sprintf("## Summary\n%s\n", m.Goal)
	}

	targets := repos
	if len(targets) == 0 {
		for name := range m.Repos {
			targets = append(targets, name)
		}
	}

	var results []ShipResult
	for _, name := range targets {
		binding, ok := m.Repos[name]
		if !ok {
			results = append(results, ShipResult{Repo: name, Error: "repo not bound to mission"})
			continue
		}

		if err := gitutil.Push(binding.IntegrationWorktree, binding.IntegrationBranch); err != nil {
			results = append(results, ShipResult{Repo: name, Error: "push: " + err.Error()})
			continue
		}

		prURL, err := CreatePR(binding.IntegrationWorktree, title, body, draft)
		if err != nil {
			results = append(results, ShipResult{Repo: name, Error: "pr: " + err.Error()})
			continue
		}
		results = append(results, ShipResult{Repo: name, PRURL: prURL})

		LinkPR(m, name, prURL)
	}
	return results
}

// LinkPR records a PR on the mission and, when a task is linked, as a
// deduplicated tracker note.
func LinkPR(m *mission.Mission, repo, prURL string) {
	oxHome := missionOxHome(m)
	mission.Update(oxHome, m.ID, func(mm *mission.Mission) error {
		for _, pr := range mm.PRs {
			if pr.URL == prURL {
				return nil
			}
		}
		mm.PRs = append(mm.PRs, mission.PRLink{Repo: repo, URL: prURL, LinkedAt: time.Now()})
		return nil
	})
	m.AppendEvent("pr_linked", "orchestrator", map[string]any{"repo": repo, "url": prURL})

	if m.Yoke != nil {
		if notes, err := yokecli.Notes(m.Yoke.ID); err == nil {
			for _, n := range notes {
				if strings.Contains(n.Content, prURL) {
					return
				}
			}
		}
		yokecli.AddNote(m.Yoke.ID, fmt.Sprintf("PR (%s): %s", repo, prURL))
	}
}

// CreatePR opens a PR via gh from the given worktree, or returns the existing
// one for the branch.
func CreatePR(worktree, title, body string, draft bool) (string, error) {
	args := []string{"pr", "create", "--title", title, "--body", body}
	if draft {
		args = append(args, "--draft")
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = worktree
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already exists") {
			return existingPR(worktree)
		}
		return "", fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return ExtractPRURL(string(output))
}

func existingPR(worktree string) (string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "url", "-q", ".url")
	cmd.Dir = worktree
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// ExtractPRURL picks the PR URL out of gh's output. CombinedOutput interleaves
// stderr warnings with the URL, so taking the whole output verbatim would
// poison downstream note content.
func ExtractPRURL(output string) (string, error) {
	url := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			url = line
		}
	}
	if url == "" {
		return "", fmt.Errorf("no PR URL in gh output: %s", strings.TrimSpace(output))
	}
	return url, nil
}

func missionOxHome(m *mission.Mission) string {
	return filepath.Dir(filepath.Dir(m.Dir()))
}
