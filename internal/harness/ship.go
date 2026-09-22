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
	Repo   string `json:"repo"`
	PRURL  string `json:"pr_url,omitempty"`
	Polish string `json:"polish,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Ship pushes each bound repo's integration branch and opens a PR. Titles and
// bodies describe the change only — nothing about the tooling leaks out.
// Notion-backed tasks must carry their ticket id as the title prefix
// ("CB-14817: ..."); ErrTicketUnknown asks the caller to get it from the user.
func Ship(cfg *config.Config, m *mission.Mission, repos []string, draft bool, title, body, ticket string) ([]ShipResult, error) {
	if title == "" {
		title = m.Goal
	}
	if body == "" {
		body = fmt.Sprintf("## Summary\n%s\n", m.Goal)
	}

	if ticket == "" {
		ticket = ResolveTicket(m)
	}
	switch {
	case ticket != "":
		if !strings.HasPrefix(title, ticket+":") {
			title = fmt.Sprintf("%s: %s", ticket, strings.TrimSpace(ticketRe.ReplaceAllString(title, "")))
		}
	case m.Yoke != nil && taskIsNotionLinked(m):
		return nil, fmt.Errorf("this task is Notion-linked but its ticket id could not be resolved — ask the user for the ticket id (like CB-14817) and pass it as ticket=")
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

		polish := PreShipPolish(cfg, m, name, binding)

		if err := gitutil.Push(binding.IntegrationWorktree, binding.IntegrationBranch); err != nil {
			results = append(results, ShipResult{Repo: name, Error: "push: " + err.Error()})
			continue
		}

		prURL, err := CreatePR(binding.IntegrationWorktree, title, body, draft)
		if err != nil {
			results = append(results, ShipResult{Repo: name, Error: "pr: " + err.Error(), Polish: polish})
			continue
		}
		results = append(results, ShipResult{Repo: name, PRURL: prURL, Polish: polish})

		LinkPR(m, name, prURL)
	}
	return results, nil
}

// MarkReady flips the mission's draft PRs to ready-for-review: it takes each
// bound repo's PR out of draft and adds the ready-for-review label. Called once
// the wider test suite is green, after ship(draft:true) opened the PR early so
// the user could test in parallel.
func MarkReady(m *mission.Mission, repos []string) []ShipResult {
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
		r := ShipResult{Repo: name}
		if msg, err := ghReady(binding.IntegrationWorktree); err != nil {
			r.Error = "ready: " + msg
		}
		results = append(results, r)
	}
	m.AppendEvent("pr_ready", "orchestrator", map[string]any{"repos": targets})
	return results
}

// ghReady takes the worktree branch's PR out of draft and labels it
// ready-for-review. Idempotent: a PR that is already non-draft is fine.
func ghReady(worktree string) (string, error) {
	ready := exec.Command("gh", "pr", "ready")
	ready.Dir = worktree
	if out, err := ready.CombinedOutput(); err != nil {
		s := strings.TrimSpace(string(out))
		if !strings.Contains(s, "not a draft") && !strings.Contains(s, "already") {
			return s, err
		}
	}
	label := exec.Command("gh", "pr", "edit", "--add-label", "ready-for-review")
	label.Dir = worktree
	if out, err := label.CombinedOutput(); err != nil {
		return strings.TrimSpace(string(out)), err
	}
	return "", nil
}

// taskIsNotionLinked reports whether the mission's task tracks a Notion page.
func taskIsNotionLinked(m *mission.Mission) bool {
	t, err := yokecli.Get(fmt.Sprintf("%d", m.Yoke.Seq))
	if err != nil {
		return false
	}
	return t.NotionURL != nil && *t.NotionURL != ""
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
		if notes, err := yokecli.Notes(fmt.Sprintf("%d", m.Yoke.Seq)); err == nil {
			for _, n := range notes {
				if strings.Contains(n.Content, prURL) {
					return
				}
			}
		}
		yokecli.AddNote(fmt.Sprintf("%d", m.Yoke.Seq), fmt.Sprintf("PR (%s): %s", repo, prURL))
	}
}

// CreatePR opens a PR via gh from the given worktree, or returns the existing
// one for the branch. A draft opens without the ready-for-review label — it is
// not ready by definition; mark_ready adds the label once the wider suite is
// green.
func CreatePR(worktree, title, body string, draft bool) (string, error) {
	args := []string{"pr", "create", "--title", title, "--body", body}
	if draft {
		args = append(args, "--draft")
	} else {
		args = append(args, "--label", "ready-for-review")
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
