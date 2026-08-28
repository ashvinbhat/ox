package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
)

var (
	planFileName       string
	planApprove        bool
	planRequestChanges string
	planComments       bool
	planNote           string
)

var planCmd = &cobra.Command{
	Use:   "plan <mission-id | task-ref>",
	Short: "Review a mission's plan from the terminal",
	Long: `Opens the plan in your editor, and delivers review verdicts to the
mission's orchestrator — a terminal review flow for plan.md and friends.

Review cycle:
  ox plan m13                        # open plan.md in your editor
  # ...add lines starting '> review: ...' next to anything you want changed...
  ox plan m13 --comments             # orc re-reads, addresses every marker
  ox plan m13 --request-changes "keep D2, drop the refactor"   # or verdict directly
  ox plan m13 --approve              # the go signal

Also works with the task ref (ox plan 124) and other artifacts
(--file decisions.md|findings.md|scratchpad.md).`,
	Args: cobra.ExactArgs(1),
	RunE: runPlan,
}

func runPlan(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()

	var m *mission.Mission
	var err error
	ref := args[0]
	if isAllDigits(ref) {
		seq, _ := strconv.Atoi(ref)
		m, err = mission.FindByYokeSeq(cfg.Home, seq)
		if err != nil {
			return fmt.Errorf("no open mission for task #%d", seq)
		}
	} else {
		m, err = mission.Open(cfg.Home, ref)
		if err != nil {
			return err
		}
	}

	path := filepath.Join(m.Dir(), planFileName)

	verdict := ""
	switch {
	case planApprove:
		verdict = fmt.Sprintf("Plan review (cli): APPROVED — proceed per %s.", planFileName)
		if planNote != "" {
			verdict += "\nNotes: " + planNote
		}
	case planRequestChanges != "":
		verdict = fmt.Sprintf("Plan review (cli): CHANGES REQUESTED on %s — address each point, update the file, and re-request approval:\n%s",
			planFileName, planRequestChanges)
	case planComments:
		verdict = fmt.Sprintf("Plan review (cli): I have edited %s directly — re-read it now. Lines starting '> review:' are my comments: address every one (change the plan, or push back with reasoning), remove resolved markers, and re-request approval. Do not build yet.",
			planFileName)
	}

	if verdict != "" {
		target := m.TmuxSession() + ":orc"
		if !tmuxutil.HasSession(m.TmuxSession()) {
			return fmt.Errorf("mission %s session is not running — `ox go %s` first", m.ID, m.ID)
		}
		if err := harness.SendMessageEnsured(target, verdict); err != nil {
			return fmt.Errorf("deliver verdict: %w", err)
		}
		fmt.Printf("Delivered to %s orchestrator:\n  %s\n", m.ID, firstPlanLine(verdict))
		return nil
	}

	// No verdict flags → open for reading/marking.
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no %s yet for %s (orchestrator writes it during planning)", planFileName, m.ID)
	}
	fmt.Printf("%s\n", path)
	if cfg.IDE != "" {
		if idePath, err := exec.LookPath(cfg.IDE); err == nil {
			return exec.Command(idePath, path).Start()
		}
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		c := exec.Command(editor, path)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	}
	return nil
}

func firstPlanLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}

func init() {
	planCmd.Flags().StringVar(&planFileName, "file", "plan.md", "Artifact to review (plan.md, decisions.md, findings.md, scratchpad.md)")
	planCmd.Flags().BoolVar(&planApprove, "approve", false, "Approve the plan (the go signal)")
	planCmd.Flags().StringVar(&planRequestChanges, "request-changes", "", "Request changes with this feedback")
	planCmd.Flags().BoolVar(&planComments, "comments", false, "Tell the orc you left '> review:' markers in the file")
	planCmd.Flags().StringVar(&planNote, "note", "", "Optional note to attach to --approve")
	rootCmd.AddCommand(planCmd)
}
