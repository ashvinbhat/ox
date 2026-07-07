package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/gitutil"
	"github.com/ashvinbhat/ox/internal/mission"
)

// BindRepos attaches repos to a mission: integration worktree + branch per
// repo, symlinked into the mission dir. Idempotent.
func BindRepos(cfg *config.Config, m *mission.Mission, names []string) error {
	for _, name := range names {
		rc, ok := cfg.Repos[name]
		if !ok {
			return fmt.Errorf("repo %q not registered", name)
		}
		if m.Repos == nil {
			m.Repos = map[string]*mission.RepoBinding{}
		}
		if _, bound := m.Repos[name]; bound {
			continue
		}

		branch := fmt.Sprintf("ox/%s-%s", m.ID, m.Slug)
		worktree := filepath.Join(cfg.Home, "worktrees", name, m.ID+"-integration")

		if _, err := os.Stat(worktree); err != nil {
			repoPath := filepath.Join(cfg.Home, "repos", name)
			baseBranch := rc.BaseBranch
			if baseBranch == "" {
				baseBranch = "origin/main"
			}
			if !strings.Contains(baseBranch, "/") {
				baseBranch = "origin/" + baseBranch
			}
			if err := withRepoLock(cfg.Home, name, func() error {
				if err := gitutil.Fetch(repoPath); err != nil {
					return fmt.Errorf("fetch %s: %w", name, err)
				}
				return gitutil.CreateWorktreeFromRef(repoPath, worktree, branch, baseBranch)
			}); err != nil {
				return err
			}
			for _, file := range rc.CopyFiles {
				copyPathHelper(filepath.Join(repoPath, file), filepath.Join(worktree, file))
			}
			runPostSetup(rc, worktree)
		}

		link := filepath.Join(m.Dir(), name)
		os.Remove(link)
		os.Symlink(worktree, link)

		m.Repos[name] = &mission.RepoBinding{IntegrationBranch: branch, IntegrationWorktree: worktree}
	}
	return m.Save()
}

type MergeResult struct {
	Worker string `json:"worker"`
	Repo   string `json:"repo"`
	Status string `json:"status"` // merged | conflict | build_failed | skipped
	Detail string `json:"detail,omitempty"`
}

// MergeWorkers merges done workers' branches into their repos' integration
// worktrees in dependency order, gating each merge on the repo's build
// command. A conflict or failed build stops that repo's pipeline.
func MergeWorkers(cfg *config.Config, m *mission.Mission, only, skip []string) ([]MergeResult, error) {
	reg, err := LoadRegistry(m)
	if err != nil {
		return nil, err
	}
	if len(m.Repos) == 0 {
		return nil, fmt.Errorf("no repos bound to this mission")
	}

	skipSet := map[string]bool{}
	for _, s := range skip {
		skipSet[s] = true
	}
	onlySet := map[string]bool{}
	for _, o := range only {
		onlySet[o] = true
	}

	ordered := topoOrder(reg)
	stopped := map[string]bool{} // repo → pipeline halted

	var results []MergeResult
	for _, w := range ordered {
		if w.Status != WorkerDone || w.BranchName == "" {
			continue
		}
		if skipSet[w.ID] || (len(onlySet) > 0 && !onlySet[w.ID]) {
			results = append(results, MergeResult{Worker: w.ID, Repo: w.Repo, Status: "skipped"})
			continue
		}
		binding, ok := m.Repos[w.Repo]
		if !ok {
			results = append(results, MergeResult{Worker: w.ID, Repo: w.Repo, Status: "skipped", Detail: "repo not bound"})
			continue
		}
		if stopped[w.Repo] {
			results = append(results, MergeResult{Worker: w.ID, Repo: w.Repo, Status: "skipped", Detail: "pipeline stopped by earlier failure"})
			continue
		}

		res := mergeOne(cfg, w, binding)
		results = append(results, res)
		if res.Status != "merged" {
			stopped[w.Repo] = true
			m.AppendEvent("merge_failed", "system", map[string]any{
				"detail": fmt.Sprintf("%s into %s: %s (%s)", w.ID, w.Repo, res.Status, res.Detail),
			})
		}
	}
	m.AppendEvent("merge_result", "orchestrator", map[string]any{"results": len(results)})
	return results, nil
}

func mergeOne(cfg *config.Config, w *Worker, binding *mission.RepoBinding) MergeResult {
	wt := binding.IntegrationWorktree

	merge := exec.Command("git", "merge", "--no-ff", w.BranchName, "-m",
		fmt.Sprintf("Merge %s", strings.TrimPrefix(w.BranchName, "ox/")))
	merge.Dir = wt
	if out, err := merge.CombinedOutput(); err != nil {
		exec.Command("git", "-C", wt, "merge", "--abort").Run()
		return MergeResult{Worker: w.ID, Repo: w.Repo, Status: "conflict", Detail: tail(string(out), 300)}
	}

	rc := cfg.Repos[w.Repo]
	if rc != nil && rc.BuildCommand != "" {
		build := exec.Command("sh", "-c", rc.BuildCommand)
		build.Dir = wt
		if out, err := build.CombinedOutput(); err != nil {
			exec.Command("git", "-C", wt, "reset", "--hard", "HEAD~1").Run()
			return MergeResult{Worker: w.ID, Repo: w.Repo, Status: "build_failed", Detail: tail(string(out), 300)}
		}
	}
	return MergeResult{Worker: w.ID, Repo: w.Repo, Status: "merged"}
}

// topoOrder sorts workers so dependencies merge before dependents.
func topoOrder(reg *Registry) []*Worker {
	var order []*Worker
	visited := map[string]bool{}

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		w, ok := reg.Workers[id]
		if !ok {
			return
		}
		for _, dep := range w.DependsOn {
			visit(dep)
		}
		order = append(order, w)
	}

	ids := make([]string, 0, len(reg.Workers))
	for id := range reg.Workers {
		ids = append(ids, id)
	}
	// Stable iteration.
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	for _, id := range ids {
		visit(id)
	}
	return order
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
