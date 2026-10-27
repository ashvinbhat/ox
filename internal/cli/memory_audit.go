package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/desktop"
	"github.com/ashvinbhat/ox/internal/memcheck"
)

var (
	auditJSON  bool
	auditIfDue bool
	auditKeep  []string
	auditNote  string
)

// The ox-brief hook calls audit --if-due at every thread start. Memories
// drift over weeks, so once a week is enough.
const auditEvery = 7 * 24 * time.Hour

var memoryAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Find memories that are stale or point at things that are gone",
	Long: `Checks the Claude Code auto-memory store (the one store; memory.db is an
archive) and lists what may need an edit:

  gone-path     a ~/ or /Users/ path that no longer exists
  gone-code     a name or source file in a code span that no repo has, in the
                checkout or at the tip of its default branch
  gone-command  an ` + "`ox <cmd>`" + ` the CLI no longer has
  gone-cite     a metadata.cites entry that no longer resolves
  index         MEMORY.md links a missing file, misses a file, or runs past
                the 200 lines Claude Code reads
  stale         a project memory nobody touched or verified in 30 days
  stranded      a memory in another project's store, which ox threads never load

Nothing is changed. Work through the list with /memory-check in a thread; it
proposes each fix and applies only what you approve. A suggestion that's fine
as it stands is kept with --keep <id> and never comes back.

The result is saved to ~/.ox/memory-check/last.json; the thread brief shows
the count.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runMemoryAudit,
}

func runMemoryAudit(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	if len(auditKeep) > 0 {
		return keepSuggestions(cfg.Home, auditKeep, auditNote)
	}
	if auditIfDue {
		if last, err := memcheck.LoadReport(cfg.Home); err == nil && time.Since(last.At) < auditEvery {
			return nil
		}
	}

	store, others := memcheck.FindStore(claudeHome(), cfg.Memory.ClaudeDir)
	if store == "" {
		return fmt.Errorf("no Claude Code memory store under %s", filepath.Join(claudeHome(), "projects"))
	}
	keep, err := memcheck.LoadKeep(cfg.Home)
	if err != nil {
		return fmt.Errorf("read keep list: %w", err)
	}
	sessions, _ := desktop.List(desktop.DefaultDir(), false)
	report, err := memcheck.Audit(memcheck.Options{
		Dir:    store,
		Others: others,
		Repos:  knownRepos(cfg.Home, sessions),
		Known:  knownOxCommand,
		Keep:   keep,
		Now:    time.Now(),
	})
	if err != nil {
		return err
	}
	if err := memcheck.SaveReport(cfg.Home, report); err != nil {
		return fmt.Errorf("save report: %w", err)
	}

	if auditJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if !auditIfDue {
		printAudit(report)
	}
	return nil
}

func keepSuggestions(oxHome string, ids []string, note string) error {
	last, err := memcheck.LoadReport(oxHome)
	if err != nil {
		return fmt.Errorf("no audit to keep from yet; run `ox memory audit` first")
	}
	open := map[string]memcheck.Suggestion{}
	for _, s := range last.Suggestions {
		open[s.ID] = s
	}
	keep, err := memcheck.LoadKeep(oxHome)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s, ok := open[id]
		if !ok {
			return fmt.Errorf("%s isn't in the last audit", id)
		}
		if s.Kind == memcheck.KindStale {
			return fmt.Errorf("%s: confirm a stale memory by setting metadata.verified to today's date, not by keeping it", id)
		}
		keep[id] = memcheck.Keep{At: time.Now().UTC(), Note: note}
	}
	if err := memcheck.SaveKeep(oxHome, keep); err != nil {
		return err
	}
	fmt.Printf("Kept %d. They won't come back.\n", len(ids))
	return nil
}

func printAudit(r memcheck.Report) {
	fmt.Printf("Memory check · %d memories · %s\n", r.Memories, r.At.Format("Mon 2 Jan 15:04"))
	fmt.Printf("Store: %s\n", tildePath(r.Dir))
	if len(r.Repos) > 0 {
		fmt.Printf("Code searched in: %s\n", strings.Join(r.Repos, ", "))
	}
	for _, n := range r.Notes {
		fmt.Printf("Note: %s\n", n)
	}
	fmt.Println()

	kept := ""
	if r.Kept > 0 {
		kept = fmt.Sprintf(" (%d kept earlier)", r.Kept)
	}
	if len(r.Suggestions) == 0 {
		fmt.Printf("Nothing to fix%s.\n", kept)
		return
	}
	fmt.Printf("%s%s:\n", countSuggestions(len(r.Suggestions)), kept)

	var strandedOnes []memcheck.Suggestion
	file := ""
	for _, s := range r.Suggestions {
		if s.Kind == memcheck.KindStranded {
			strandedOnes = append(strandedOnes, s)
			continue
		}
		if s.File != file {
			file = s.File
			fmt.Printf("\n%s\n", file)
		}
		line := s.Detail
		if s.Subject != "" && s.Kind != memcheck.KindIndex {
			line = s.Subject + " — " + s.Detail
		}
		fmt.Printf("  %-13s %s\n", s.Kind, line)
	}
	if len(strandedOnes) > 0 {
		fmt.Println("\nStranded in other projects' stores (ox threads never load these):")
		for _, s := range strandedOnes {
			fmt.Printf("  %s\n", strings.TrimPrefix(s.File, filepath.Join(claudeHome(), "projects")+"/"))
		}
	}
	fmt.Println("\nNext: /memory-check in any thread walks you through them, five at a time.")
}

func countSuggestions(n int) string {
	if n == 1 {
		return "1 suggestion"
	}
	return fmt.Sprintf("%d suggestions", n)
}

// knownOxCommand reports whether `ox <words...>` resolves to a command.
func knownOxCommand(words []string) bool {
	cmd, rest, err := rootCmd.Find(words)
	if err != nil || cmd == rootCmd {
		return false
	}
	// A command group given a word it doesn't know: that sub-command is gone.
	return cmd.Runnable() || len(rest) == 0
}

func claudeHome() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

func init() {
	memoryAuditCmd.Flags().BoolVar(&auditJSON, "json", false, "Machine-readable output, with suggestion ids")
	memoryAuditCmd.Flags().BoolVar(&auditIfDue, "if-due", false, "Skip if an audit ran in the last week")
	memoryAuditCmd.Flags().StringArrayVar(&auditKeep, "keep", nil, "Keep a suggestion as is (id from --json); repeatable")
	memoryAuditCmd.Flags().StringVar(&auditNote, "note", "", "Why it's kept, with --keep")
	memoryCmd.AddCommand(memoryAuditCmd)
}
