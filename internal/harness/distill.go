package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/memory"
	"github.com/ashvinbhat/ox/internal/memory/embed"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

// CloseMission finalizes a mission: kills leftover worker sessions, releases
// locks, runs the distiller, prunes worker worktrees (integration branches
// survive until their PRs land), and archives stale memories.
func CloseMission(cfg *config.Config, m *mission.Mission) error {
	reg, err := LoadRegistry(m)
	if err == nil {
		for _, w := range reg.Workers {
			if tmuxutil.HasSession(w.TmuxSession) {
				tmuxutil.KillSession(w.TmuxSession)
			}
			if !w.Finished() {
				MarkWorkerFinished(cfg.Home, m, w.ID, WorkerKilled, "mission closed")
			}
		}
	}

	// Distillation is a minutes-long headless model call — detached so the
	// close returns in seconds. An interrupted close used to strand the
	// whole teardown behind a spinner the user would kill.
	distill := exec.Command(oxBinary(), "missions", "distill", m.ID)
	distill.SysProcAttr = detachedProc()
	distill.Stdout, distill.Stderr = nil, nil
	if err := distill.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "distiller launch: %v\n", err)
		m.AppendEvent("distill_failed", "system", map[string]any{"error": err.Error()})
	} else {
		go distill.Wait()
	}

	if reg != nil {
		for _, w := range reg.Workers {
			RemoveWorkerWorktree(cfg, w)
		}
	}
	PruneIntegration(cfg, m)
	PruneReviewWorktree(cfg, m)

	if store, err := memory.Open(cfg.Home, nil); err == nil {
		store.GC(nil)
		store.Close()
	}

	if m.Yoke != nil {
		summaryPath := filepath.Join(m.Dir(), "summary.md")
		note := fmt.Sprintf("[mission %s closed] %s", m.ID, m.Outcome)
		if _, err := os.Stat(summaryPath); err == nil {
			note += " Summary: " + summaryPath
		}
		yokecli.AddNote(fmt.Sprintf("%d", m.Yoke.Seq), note)
	}

	m.AppendEvent("mission_closed", "system", map[string]any{"outcome": m.Outcome})
	return nil
}

type distillOutput struct {
	MissionSummaryMD string `json:"mission_summary_md"`
	Memories         []struct {
		Kind       string   `json:"kind"`
		Scope      string   `json:"scope"`
		Title      string   `json:"title"`
		Content    string   `json:"content"`
		Tags       []string `json:"tags"`
		Supersedes string   `json:"supersedes"`
	} `json:"memories"`
	RepoDocs map[string]string `json:"repo_docs"`
}

// RunDistiller extracts durable knowledge from a finished mission via one
// cheap headless job: mission summary, ≤8 memories (deduped on write), and
// full-rewrite repo doc revisions.
func RunDistiller(cfg *config.Config, m *mission.Mission) error {
	evidence := collectEvidence(cfg, m)
	if strings.TrimSpace(evidence) == "" {
		return nil
	}

	prompt := distillerPrompt(m, evidence)
	j, err := job.Start(cfg, m, job.StartInput{
		ID: fmt.Sprintf("distiller-%d", time.Now().Unix()), Prompt: prompt, Model: cfg.JobModel(),
		MaxTurns: 8, MaxBudgetUSD: 1.0, ExpectJSON: true,
	})
	if err != nil {
		return err
	}

	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		job.Harvest(cfg, m, j)
		if j.Status == job.StatusDone {
			break
		}
		if j.Status == job.StatusFailed {
			if retried, _ := job.RetryOrEscalate(cfg, m, j); retried == nil {
				return fmt.Errorf("distiller failed: %s", j.Error)
			} else {
				j = retried
			}
		}
	}
	if j.Status != job.StatusDone {
		return fmt.Errorf("distiller timed out")
	}

	raw, err := job.Result(m, j)
	if err != nil {
		return err
	}
	var out distillOutput
	if err := json.Unmarshal([]byte(stripFences(raw)), &out); err != nil {
		return fmt.Errorf("parse distill output: %w", err)
	}

	if out.MissionSummaryMD != "" {
		os.WriteFile(filepath.Join(m.Dir(), "summary.md"), []byte(out.MissionSummaryMD), 0o644)
	}

	store, err := memory.Open(cfg.Home, embed.New(cfg.Memory.Embeddings))
	if err == nil {
		defer store.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		kept := 0
		for _, mem := range out.Memories {
			if kept >= 8 || mem.Content == "" {
				continue
			}
			res, err := store.Remember(ctx, memory.RememberInput{
				Content: mem.Content, Kind: mem.Kind, Scope: mem.Scope, Title: mem.Title,
				Tags: mem.Tags, Supersedes: mem.Supersedes,
				Source: "mission:" + m.ID + "/distiller",
			})
			if err == nil && res.Status == "created" {
				kept++
			}
		}
	}

	for repo, doc := range out.RepoDocs {
		if strings.HasPrefix(strings.TrimSpace(doc), "NO_CHANGES") {
			continue
		}
		if _, bound := m.Repos[repo]; !bound {
			continue
		}
		if err := WriteRepoDoc(cfg.Home, repo, doc, "mission:"+m.ID); err != nil {
			fmt.Fprintf(os.Stderr, "repo doc %s: %v\n", repo, err)
		}
	}
	return nil
}

func collectEvidence(cfg *config.Config, m *mission.Mission) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "MISSION: %s — %s (playbook %s, outcome: %s)\n\n", m.ID, m.Goal, m.Type, m.Outcome)

	appendFile := func(label, path string, cap int) {
		data, err := os.ReadFile(path)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			return
		}
		s := string(data)
		if len(s) > cap {
			s = s[len(s)-cap:]
		}
		fmt.Fprintf(&sb, "=== %s ===\n%s\n\n", label, s)
	}

	appendFile("PLAN", filepath.Join(m.Dir(), "plan.md"), 8000)
	appendFile("DECISIONS", filepath.Join(m.Dir(), "decisions.md"), 4000)
	appendFile("SCRATCHPAD", filepath.Join(m.Dir(), "scratchpad.md"), 6000)

	workers, _ := os.ReadDir(filepath.Join(m.Dir(), "workers"))
	for _, w := range workers {
		appendFile("WORKER OUTPUT: "+w.Name(), filepath.Join(m.Dir(), "workers", w.Name(), "output.md"), 3000)
	}

	for repo, binding := range m.Repos {
		diffstat := exec.Command("git", "-C", binding.IntegrationWorktree, "diff", "--stat", "HEAD~10..HEAD")
		if out, err := diffstat.Output(); err == nil && len(out) > 0 {
			fmt.Fprintf(&sb, "=== DIFFSTAT %s ===\n%.2000s\n\n", repo, string(out))
		}
		if doc := RepoDoc(cfg.Home, repo); doc != "" {
			fmt.Fprintf(&sb, "=== CURRENT REPO DOC %s ===\n%s\n\n", repo, doc)
		}
	}

	if store, err := memory.Open(cfg.Home, embed.New(cfg.Memory.Embeddings)); err == nil {
		defer store.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var scopes []string
		for repo := range m.Repos {
			scopes = append(scopes, "repo:"+repo)
		}
		if mems, _, err := store.Search(ctx, m.Goal, memory.SearchOptions{Scopes: scopes, K: 20}); err == nil && len(mems) > 0 {
			sb.WriteString("=== EXISTING MEMORIES (do NOT restate; supersede by uid when contradicted) ===\n")
			for _, mem := range mems {
				fmt.Fprintf(&sb, "- uid=%s [%s/%s] %s\n", mem.UID, mem.Kind, mem.Scope, mem.Content)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func distillerPrompt(m *mission.Mission, evidence string) string {
	scopeHint := "global"
	for repo := range m.Repos {
		scopeHint = "repo:" + repo
		break
	}
	return fmt.Sprintf(`You are the ox distiller. Extract durable knowledge from this finished mission.

Reply with ONLY a JSON object (no prose, no fences):
{
  "mission_summary_md": "<= 60 lines: what happened, what shipped, what's unresolved",
  "memories": [
    { "kind": "gotcha|learning|convention|architecture|decision|tool|profile|failure",
      "scope": "%s or task:%d or global",
      "title": "<= 80 chars",
      "content": "self-contained, <= 120 tokens; a stranger with no mission context must be able to act on it",
      "tags": ["..."],
      "supersedes": "<uid from EXISTING MEMORIES, only if this replaces it, else omit>" }
  ],
  "repo_docs": { "<repo>": "<full revised doc, <= 300 lines>" }
}

Rules:
- 0..8 memories. Zero is valid. Only knowledge that is true next month and useful to an
  agent who never saw this mission. No task status, no transient state.
- Do NOT restate anything in EXISTING MEMORIES; supersede via uid when contradicted.
- Prefer gotchas and conventions over generic lessons.
- repo_docs: full rewrite or the literal string "NO_CHANGES: <reason>". Sections:
  Architecture / Conventions / Build, test, run / Gotchas / Key files. Fold in confirmed
  discoveries; delete anything this mission proved stale.

%s`, scopeHint, yokeSeq(m), evidence)
}

func yokeSeq(m *mission.Mission) int {
	if m.Yoke != nil {
		return m.Yoke.Seq
	}
	return 0
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	return strings.TrimSpace(strings.TrimSuffix(s, "```"))
}

// PruneReviewWorktree removes a review mission's PR-head checkout (recorded
// in review.json by prepare_review). Review worktrees are keyed by PR, not
// mission — two missions on the same PR share one — so the checkout is only
// dropped when no OPEN mission still references it. A follow-up round
// recreates it from the PR automatically anyway.
func PruneReviewWorktree(cfg *config.Config, m *mission.Mission) {
	rc, ok := reviewContextOf(m)
	if !ok {
		return
	}
	missions, err := mission.List(cfg.Home)
	if err == nil {
		for _, other := range missions {
			if !other.Open() || other.ID == m.ID {
				continue
			}
			if orc, ok := reviewContextOf(other); ok && orc.Worktree == rc.Worktree {
				return // an open mission is still reviewing this PR
			}
		}
	}
	removeWorktree(cfg, rc.RepoName, rc.Worktree)
}

type reviewRef struct {
	RepoName string `json:"repo_name"`
	Worktree string `json:"worktree"`
}

func reviewContextOf(m *mission.Mission) (reviewRef, bool) {
	data, err := os.ReadFile(filepath.Join(m.Dir(), "review.json"))
	if err != nil {
		return reviewRef{}, false
	}
	var rc reviewRef
	if json.Unmarshal(data, &rc) != nil || rc.Worktree == "" {
		return reviewRef{}, false
	}
	return rc, true
}

// RemoveWorkerWorktree cleans a worker's worktree from its base repo. Workers
// running in a shared directory never own it — nothing to remove.
func RemoveWorkerWorktree(cfg *config.Config, w *Worker) {
	if w.SharedCwd {
		return
	}
	removeWorktree(cfg, w.Repo, w.WorktreePath)
}

// PruneIntegration removes a mission's integration worktrees once they are
// safe to drop: every PR on that repo is merged or closed, or the repo
// shipped no PR at all (nothing references the branch). Open PRs keep their
// worktree — review fixes push from it. Returns the repos pruned.
func PruneIntegration(cfg *config.Config, m *mission.Mission) []string {
	var pruned []string
	for repo, binding := range m.Repos {
		if binding.IntegrationWorktree == "" {
			continue
		}
		if _, err := os.Stat(binding.IntegrationWorktree); err != nil {
			continue
		}

		safe := true
		for _, pr := range m.PRs {
			if pr.Repo != repo {
				continue
			}
			out, err := exec.Command("gh", "pr", "view", pr.URL, "--json", "state", "-q", ".state").Output()
			if err != nil {
				safe = false // can't verify → keep
				break
			}
			state := strings.TrimSpace(string(out))
			if state != "MERGED" && state != "CLOSED" {
				safe = false
				break
			}
		}
		if !safe {
			continue
		}
		removeWorktree(cfg, repo, binding.IntegrationWorktree)
		os.Remove(filepath.Join(m.Dir(), repo))
		pruned = append(pruned, repo)
	}
	return pruned
}

func removeWorktree(cfg *config.Config, repo, worktreePath string) {
	if worktreePath == "" {
		return
	}
	repoPath := filepath.Join(cfg.Home, "repos", repo)
	withRepoLock(cfg.Home, repo, func() error {
		exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreePath).Run()
		os.RemoveAll(worktreePath)
		return nil
	})
}

// detachedProc lets a spawned process outlive its parent — the detached
// distiller must survive the MCP server that launched it.
func detachedProc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
