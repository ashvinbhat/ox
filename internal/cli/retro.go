package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/job"
	"github.com/ashvinbhat/ox/internal/mission"
)

var retroCmd = &cobra.Command{
	Use:   "retro [mission-id | task-ref]",
	Short: "Mission scorecards — what the harness actually cost and did",
	Long: `The improvement flywheel: per-mission metrics computed from the files the
harness already writes (ledger, events, agents, jobs, phase history). No
argument = one line per mission, newest first. With a mission: the full
scorecard — phase timeline, cost by actor kind, workers, jobs, PR lead time.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRetro,
}

type retroStats struct {
	m           *mission.Mission
	duration    time.Duration
	cost        float64
	costByKind  map[string]float64
	workers     int
	interrupted int
	respawns    int
	fixWorkers  int
	jobs        int
	jobsFailed  int
	jobsCost    float64
	wakes       int
	mergeLead   time.Duration
	prsMerged   int
}

func gatherRetro(m *mission.Mission) *retroStats {
	st := &retroStats{m: m, costByKind: map[string]float64{}}

	end := time.Now()
	if m.ClosedAt != nil {
		end = *m.ClosedAt
	}
	st.duration = end.Sub(m.CreatedAt)
	st.cost = m.SpentUSD

	if f, err := os.Open(filepath.Join(m.Dir(), "ledger.jsonl")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var e struct {
				Kind string  `json:"kind"`
				Cost float64 `json:"cost_usd"`
			}
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				st.costByKind[e.Kind] += e.Cost
			}
		}
		f.Close()
	}

	if reg, err := harness.LoadRegistry(m); err == nil {
		st.workers = len(reg.Workers)
	}

	starts := map[string]int{}
	linked := map[string]time.Time{}
	if events, err := m.EventsSince(0); err == nil {
		for _, ev := range events {
			id, _ := ev.Data["id"].(string)
			switch ev.Type {
			case "agent_started":
				starts[id]++
			case "agent_interrupted":
				st.interrupted++
			case "agent_spawned", "agent_done":
				// fix-round signal: follow-up workers named for fixing
				if ev.Type == "agent_spawned" && containsFix(id) {
					st.fixWorkers++
				}
			case "pr_linked":
				if url, _ := ev.Data["url"].(string); url != "" {
					linked[url] = ev.TS
				}
			case "pr_merged":
				st.prsMerged++
				if url, _ := ev.Data["url"].(string); url != "" {
					if t0, ok := linked[url]; ok {
						st.mergeLead = ev.TS.Sub(t0)
					}
				}
			}
		}
	}
	for _, n := range starts {
		if n > 1 {
			st.respawns += n - 1
		}
	}

	if idx, err := job.LoadIndex(m); err == nil {
		for _, j := range idx.Jobs {
			st.jobs++
			st.jobsCost += j.CostUSD
			if j.Status == job.StatusFailed {
				st.jobsFailed++
			}
		}
	}
	return st
}

func containsFix(id string) bool {
	for i := 0; i+3 <= len(id); i++ {
		if id[i:i+3] == "fix" {
			return true
		}
	}
	return false
}

func runRetro(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	if len(args) == 1 {
		var m *mission.Mission
		var err error
		if isAllDigits(args[0]) {
			seq, _ := strconv.Atoi(args[0])
			m, err = mission.FindByYokeSeq(cfg.Home, seq)
		} else {
			m, err = mission.Open(cfg.Home, args[0])
		}
		if err != nil {
			return err
		}
		printRetroDetail(gatherRetro(m))
		return nil
	}

	missions, err := mission.List(cfg.Home)
	if err != nil {
		return err
	}
	sort.Slice(missions, func(i, j int) bool { return missions[i].CreatedAt.After(missions[j].CreatedAt) })

	fmt.Printf("%-6s %-11s %-8s %8s  %-13s %-9s %-6s %s\n",
		"ID", "STATE", "AGE/DUR", "COST", "WORKERS", "JOBS", "PRS", "GOAL")
	for _, m := range missions {
		st := gatherRetro(m)
		state := m.Phase
		if !m.Open() {
			state = "✓ " + m.Outcome
		}
		workers := fmt.Sprintf("%d", st.workers)
		if st.respawns > 0 || st.fixWorkers > 0 {
			workers += fmt.Sprintf(" (%dre/%dfx)", st.respawns, st.fixWorkers)
		}
		jobs := fmt.Sprintf("%d", st.jobs)
		if st.jobsFailed > 0 {
			jobs += fmt.Sprintf(" (%d✗)", st.jobsFailed)
		}
		prs := fmt.Sprintf("%d/%d", st.prsMerged, len(m.PRs))
		fmt.Printf("%-6s %-11s %-8s %7.2f$  %-13s %-9s %-6s %s\n",
			m.ID, firstN(state, 11), shortDur(st.duration), st.cost, workers, jobs, prs, firstN(m.Goal, 44))
	}
	return nil
}

func printRetroDetail(st *retroStats) {
	m := st.m
	fmt.Printf("Mission %s — %s\n", m.ID, m.Goal)
	state := m.Phase
	if !m.Open() {
		state = "closed: " + m.Outcome
	}
	fmt.Printf("  state:     %s · duration %s · total $%.2f\n", state, shortDur(st.duration), st.cost)

	if len(m.PhaseHistory) > 0 {
		fmt.Println("  phases:")
		for i, ph := range m.PhaseHistory {
			end := time.Now()
			if i+1 < len(m.PhaseHistory) {
				end = m.PhaseHistory[i+1].At
			} else if m.ClosedAt != nil {
				end = *m.ClosedAt
			}
			fmt.Printf("    %-12s %s\n", ph.Phase, shortDur(end.Sub(ph.At)))
		}
	}

	if len(st.costByKind) > 0 {
		fmt.Println("  cost by actor:")
		kinds := make([]string, 0, len(st.costByKind))
		for k := range st.costByKind {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return st.costByKind[kinds[i]] > st.costByKind[kinds[j]] })
		for _, k := range kinds {
			fmt.Printf("    %-12s $%.2f\n", k, st.costByKind[k])
		}
	}

	fmt.Printf("  workers:   %d spawned · %d interrupted · %d respawns · %d fix-workers\n",
		st.workers, st.interrupted, st.respawns, st.fixWorkers)
	fmt.Printf("  jobs:      %d run · %d failed · $%.2f\n", st.jobs, st.jobsFailed, st.jobsCost)
	if len(m.PRs) > 0 {
		lead := "-"
		if st.mergeLead > 0 {
			lead = shortDur(st.mergeLead)
		}
		fmt.Printf("  PRs:       %d linked · %d merged · ship→merge %s\n", len(m.PRs), st.prsMerged, lead)
	}
}

func shortDur(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

func init() {
	rootCmd.AddCommand(retroCmd)
}
