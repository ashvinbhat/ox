package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/asks"
	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/park"
	"github.com/ashvinbhat/ox/internal/work"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

// quietFor is how long a thread sits untouched before the daily routine
// archives it.
const quietFor = 24 * time.Hour

var archivableJSON bool

type archiveCandidate struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	LastActive time.Time `json:"lastActive"`
}

var workArchivableCmd = &cobra.Command{
	Use:   "archivable",
	Short: "The threads the daily routine archives",
	Long: `Lists the threads to archive. A thread qualifies when all of these hold:
its last turn finished with nothing waiting on you, it isn't parked, none of
its PRs is open on GitHub, all its work in yoke is done or dropped, its own
worktree has no uncommitted or unpushed changes, and it has been quiet for 24
hours. The daily-archive skill archives these, then runs ` + "`ox work archived`" + `.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		sessions, err := desktop.List(desktop.DefaultDir(), false)
		if err != nil {
			return fmt.Errorf("read desktop threads: %w", err)
		}
		tasks, err := yokecli.List(true, "")
		if err != nil {
			return err
		}
		parked, _ := park.Load(cfg.Home)
		open, _, _ := asks.LoadPRs(cfg.Home)
		found := archivable(sessions, parked, tasks, open, time.Now(), worktreeDirty)
		if archivableJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(found)
		}
		for _, c := range found {
			fmt.Printf("%s  %s\n", c.ID, c.Title)
		}
		if len(found) == 0 {
			fmt.Println("Nothing to archive.")
		}
		return nil
	},
}

var workArchivedCmd = &cobra.Command{
	Use:   "archived <thread-id>...",
	Short: "Record threads the daily routine archived, for the next brief",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := requireConfig()
		sessions, _ := desktop.List(desktop.DefaultDir(), true)
		titles := map[string]string{}
		for _, s := range sessions {
			titles[s.ID] = s.Title
		}
		now := time.Now()
		var rec []archiveCandidate
		for _, id := range args {
			rec = append(rec, archiveCandidate{ID: id, Title: titles[id], LastActive: now})
		}
		if err := saveArchived(cfg.Home, now, rec); err != nil {
			return err
		}
		fmt.Printf("Recorded %d archived.\n", len(rec))
		return nil
	},
}

// archivable applies the routine's rules. dirty reports a worktree with
// uncommitted or unpushed changes; only a thread's own worktree counts, as a
// shared checkout's changes can't be told apart by thread.
func archivable(sessions []desktop.Session, parked map[string]park.Record, tasks []yokecli.Task,
	open []asks.PR, now time.Time, dirty func(dir string) bool) []archiveCandidate {
	isOpen := map[string]bool{}
	for _, p := range open {
		isOpen[p.Ref()] = true
	}
	openWork := map[string]bool{}
	for _, t := range tasks {
		if t.Status == yokecli.StatusDone || t.Status == yokecli.StatusDropped {
			continue
		}
		for _, k := range t.Keys {
			openWork[strings.ToLower(k)] = true
		}
	}
	var out []archiveCandidate
	for _, s := range sessions {
		if !s.Finished() || now.Sub(s.LastActive()) < quietFor {
			continue
		}
		if r, ok := parked[s.ID]; ok && r.Holds(s.LastActive()) {
			continue
		}
		if openWork[strings.ToLower(work.ThreadKey(s.ID))] || hasOpenPR(s, isOpen) {
			continue
		}
		if ownWorktree(s.Cwd) && dirty(s.Cwd) {
			continue
		}
		out = append(out, archiveCandidate{ID: s.ID, Title: s.Title, LastActive: s.LastActive()})
	}
	return out
}

func hasOpenPR(s desktop.Session, isOpen map[string]bool) bool {
	for _, p := range s.PRs {
		if isOpen[prRef(p.Repo, p.Number)] {
			return true
		}
	}
	return false
}

func ownWorktree(dir string) bool {
	return strings.Contains(filepath.ToSlash(dir), "/.claude/worktrees/")
}

// worktreeDirty reports uncommitted files, or commits on no remote branch.
// A worktree already gone has nothing to lose.
func worktreeDirty(dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	status, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil || len(strings.TrimSpace(string(status))) > 0 {
		return true
	}
	ahead, err := exec.Command("git", "-C", dir, "log", "--oneline", "HEAD", "--not", "--remotes").Output()
	return err != nil || len(strings.TrimSpace(string(ahead))) > 0
}

func archivedPath(oxHome string, day time.Time) string {
	return filepath.Join(oxHome, "archived", day.Format("2006-01-02")+".json")
}

func saveArchived(oxHome string, now time.Time, rec []archiveCandidate) error {
	path := archivedPath(oxHome, now)
	var all []archiveCandidate
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	all = append(all, rec...)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// archivedLine is the brief's word on what the routine archived today or
// yesterday.
func archivedLine(oxHome string, now time.Time) string {
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		data, err := os.ReadFile(archivedPath(oxHome, day))
		if err != nil {
			continue
		}
		var rec []archiveCandidate
		if json.Unmarshal(data, &rec) != nil || len(rec) == 0 {
			continue
		}
		sort.Slice(rec, func(i, j int) bool { return rec[i].LastActive.Before(rec[j].LastActive) })
		var titles []string
		for _, r := range rec[:min(3, len(rec))] {
			titles = append(titles, clip(r.Title, 30))
		}
		if len(rec) > 3 {
			titles = append(titles, fmt.Sprintf("+%d", len(rec)-3))
		}
		when := "today"
		if day.Format("2006-01-02") != now.Format("2006-01-02") {
			when = "yesterday"
		}
		return fmt.Sprintf("Archived %s: %s (%s)", when, countOf(len(rec), "thread"), strings.Join(titles, ", "))
	}
	return ""
}

func init() {
	workArchivableCmd.Flags().BoolVar(&archivableJSON, "json", false, "Machine-readable output")
	workCmd.AddCommand(workArchivableCmd, workArchivedCmd)
}
