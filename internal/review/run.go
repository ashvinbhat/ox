package review

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// AgentResult is one reviewer's output: its spec, findings, addressing
// entries, log path, and any execution error.
type AgentResult struct {
	Spec       AgentSpec
	Findings   []Finding
	Addressing []Addressing
	LogPath    string
	Err        error
}

// RunAll fans the given reviewer specs out in parallel against the same
// review worktree. Each agent's stdout/stderr is captured to its own log
// file under .ox/review/logs/<agent>.log so terminals don't interleave.
// Progress lines are written to `progress`.
//
// priorRefsByAgent maps an agent name (correctness/security/design/test) to
// the slice of prior-finding refs that were originally produced by THAT
// agent. On a first-time review this map is empty; on a follow-up review
// each agent receives only its own prior refs (so verdicts stay
// category-coherent).
//
// concurrency caps how many agents run at once. <= 0 means "no cap" (all
// in parallel) which matches the v1 default.
//
// Returns the deduped aggregated findings, the merged addressing entries,
// and the per-agent results (so callers can surface log paths on failure).
func RunAll(worktree *ReviewWorktree, specs []AgentSpec, priorRefsByAgent map[string][]string, concurrency int, progress io.Writer) ([]Finding, []Addressing, []AgentResult, error) {
	logsDir := filepath.Join(worktree.Path, ".ox", "review", "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("create logs dir: %w", err)
	}

	if progress == nil {
		progress = io.Discard
	}

	header := "in parallel"
	if concurrency > 0 && concurrency < len(specs) {
		header = fmt.Sprintf("with concurrency=%d", concurrency)
	}
	fmt.Fprintf(progress, "Running %d reviewers %s:\n", len(specs), header)
	for _, s := range specs {
		ann := ""
		if refs := priorRefsByAgent[s.Name]; len(refs) > 0 {
			ann = fmt.Sprintf("  (with %d prior finding(s) to address)", len(refs))
		}
		fmt.Fprintf(progress, "  · %s — %s%s\n", s.Name, s.Focus, ann)
	}
	fmt.Fprintln(progress)

	// Semaphore for concurrency cap. nil → unlimited.
	var sem chan struct{}
	if concurrency > 0 && concurrency < len(specs) {
		sem = make(chan struct{}, concurrency)
	}

	results := make([]AgentResult, len(specs))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, spec := range specs {
		wg.Add(1)
		go func(i int, spec AgentSpec) {
			defer wg.Done()
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			logPath := filepath.Join(logsDir, spec.Name+".log")
			out, err := RunAgent(worktree, spec, logPath, priorRefsByAgent[spec.Name])

			mu.Lock()
			defer mu.Unlock()
			results[i] = AgentResult{
				Spec:       spec,
				Findings:   out.Findings,
				Addressing: out.Addressing,
				LogPath:    logPath,
				Err:        err,
			}
			if err != nil {
				fmt.Fprintf(progress, "  ✗ %s — failed: %v\n     log: %s\n", spec.Name, err, logPath)
			} else if refs := priorRefsByAgent[spec.Name]; len(refs) > 0 {
				fmt.Fprintf(progress, "  ✓ %s — %d new finding(s), %d addressing verdict(s)\n", spec.Name, len(out.Findings), len(out.Addressing))
			} else {
				fmt.Fprintf(progress, "  ✓ %s — %d finding(s)\n", spec.Name, len(out.Findings))
			}
		}(i, spec)
	}
	wg.Wait()
	fmt.Fprintln(progress)

	var allFindings []Finding
	var allAddressing []Addressing
	for _, r := range results {
		allFindings = append(allFindings, r.Findings...)
		allAddressing = append(allAddressing, r.Addressing...)
	}
	return Dedupe(allFindings), allAddressing, results, nil
}
