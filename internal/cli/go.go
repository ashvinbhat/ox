package cli

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ashvinbhat/ox/internal/harness"
	"github.com/ashvinbhat/ox/internal/mission"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
	"github.com/ashvinbhat/ox/internal/yokecli"
)

var (
	goPlaybook string
	goModel    string
	goNoAttach bool
)

var goCmd = &cobra.Command{
	Use:   "go [task-ref | mission-id | \"freeform goal\"]",
	Short: "Open (or resume) a mission with the orchestrator",
	Long: `Starts a persistent orchestrator session for any kind of agentic work.

The orchestrator plans with you, spawns worker agents only when needed, watches
them, integrates, and ships — all in one tmux session you can leave and resume
at any time. Sessions are tied to the mission (and its task): closing tmux or
rebooting loses nothing.

References:
  ox go 114          # yoke task — resumes its open mission or starts one
  ox go m17          # resume mission m17
  ox go "why is CI flaky since tuesday" --playbook debug
  ox go              # pick from open missions

Playbooks define the mission type (task, debug, ...). Add your own at
~/.ox/playbooks/<name>.md.`,
	Args: cobra.ArbitraryArgs,
	RunE: runGo,
}

var missionIDRe = regexp.MustCompile(`^m\d+$`)

func runGo(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	if !tmuxutil.IsAvailable() {
		return fmt.Errorf("tmux is required for missions")
	}

	ref := strings.TrimSpace(strings.Join(args, " "))

	switch {
	case ref == "":
		return pickMission(cfg.Home)

	case missionIDRe.MatchString(ref):
		m, err := mission.Open(cfg.Home, ref)
		if err != nil {
			return err
		}
		return launchMission(cfg.Home, m, true)

	case isAllDigits(ref):
		return goYokeTask(cfg.Home, ref)

	default:
		return createMission(cfg.Home, goPlaybook, ref, nil, "")
	}
}

func goYokeTask(oxHome, ref string) error {
	seq, _ := strconv.Atoi(ref)
	if m, err := mission.FindByYokeSeq(oxHome, seq); err == nil {
		fmt.Printf("Resuming mission %s for task #%d: %s\n", m.ID, seq, m.Goal)
		return launchMission(oxHome, m, true)
	}

	t, err := yokecli.Get(ref)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	taskMD, err := yokecli.ContextMarkdown(ref)
	if err != nil {
		fmt.Printf("Warning: could not load task context: %v\n", err)
	}

	if t.Status != yokecli.StatusInProgress {
		if err := yokecli.Start(t.ID); err != nil {
			fmt.Printf("Warning: failed to start task: %v\n", err)
		}
	}

	return createMission(oxHome, goPlaybook, t.Title, &mission.YokeRef{ID: t.ID, Seq: t.Seq}, taskMD)
}

func createMission(oxHome, playbook, goal string, yoke *mission.YokeRef, taskMD string) error {
	if !harness.PlaybookExists(oxHome, playbook) {
		return fmt.Errorf("unknown playbook %q — add ~/.ox/playbooks/%s.md or use --playbook task|debug", playbook, playbook)
	}

	model := goModel
	if model == "" {
		model = orchestratorModel()
	}

	m, err := mission.Create(oxHome, playbook, goal, yoke, model, uuid.NewString())
	if err != nil {
		return fmt.Errorf("create mission: %w", err)
	}

	if err := harness.WriteOrchestratorFiles(oxHome, m, taskMD); err != nil {
		return err
	}
	if err := harness.WriteMCPConfig(m); err != nil {
		return err
	}
	linkYokeDocs(m.Dir())

	fmt.Printf("Mission %s created: %s\n", m.ID, m.Goal)
	fmt.Printf("  Dir: %s\n", m.Dir())
	fmt.Printf("  Playbook: %s · Orchestrator: %s\n", m.Type, m.Orchestrator.Model)

	if yoke != nil {
		yokecli.AddNote(yoke.ID, fmt.Sprintf("[mission %s opened] playbook=%s", m.ID, m.Type))
	}

	return launchMission(oxHome, m, false)
}

// launchMission ensures the mission tmux session exists with the orchestrator
// running (fresh or resumed), then attaches.
func launchMission(oxHome string, m *mission.Mission, resume bool) error {
	if !m.Open() {
		return fmt.Errorf("mission %s is closed (%s)", m.ID, m.Outcome)
	}

	session := m.TmuxSession()

	// Regenerate wiring that older missions may predate (and pick up prompt/
	// playbook changes) — cheap and idempotent.
	if err := harness.WriteMCPConfig(m); err != nil {
		return err
	}
	if _, err := os.Stat(m.Dir() + "/orchestrator-prompt.md"); err != nil {
		if err := harness.WriteOrchestratorFiles(oxHome, m, ""); err != nil {
			return err
		}
	}

	// Windows are targeted by name, not index — user tmux configs often set
	// base-index 1, so ":0" is not a safe target.
	orcTarget := session + ":orc"

	if !tmuxutil.HasSession(session) {
		if err := tmuxutil.NewSession(session, m.Dir()); err != nil {
			return fmt.Errorf("create tmux session: %w", err)
		}
		tmuxutil.RenameWindow(session, "orc")
		tmuxutil.SetEnv(session, "OX_MISSION_ID", m.ID)
		if err := tmuxutil.SendKeys(orcTarget, orchestratorCmd(m, resume)); err != nil {
			return fmt.Errorf("launch orchestrator: %w", err)
		}
		m.AppendEvent("orchestrator_launched", "system", map[string]any{"resume": resume})
	} else if !tmuxutil.HasWindow(session, "orc") {
		if err := tmuxutil.NewWindow(session, "orc", m.Dir(), ""); err != nil {
			return fmt.Errorf("create orc window: %w", err)
		}
		if err := tmuxutil.SendKeys(orcTarget, orchestratorCmd(m, true)); err != nil {
			return fmt.Errorf("relaunch orchestrator: %w", err)
		}
		m.AppendEvent("orchestrator_launched", "system", map[string]any{"resume": true})
	} else if orchestratorDead(session) {
		if err := tmuxutil.SendKeys(orcTarget, orchestratorCmd(m, true)); err != nil {
			return fmt.Errorf("relaunch orchestrator: %w", err)
		}
		m.AppendEvent("orchestrator_launched", "system", map[string]any{"resume": true})
	}

	if !harness.EnsureClaudeReady(orcTarget, 45*time.Second) {
		fmt.Println("Warning: orchestrator did not reach the input prompt in time — check the tmux window")
	}

	if goNoAttach {
		fmt.Printf("Mission session: %s (not attaching)\n", session)
		return nil
	}
	if tmuxutil.InsideTmux() {
		return tmuxutil.SwitchClient(session)
	}
	return tmuxutil.Attach(session)
}

// orchestratorCmd builds the shell command for the orc window. Resume falls
// back to a fresh session when no transcript exists yet (session id was
// pre-assigned but claude never ran).
func orchestratorCmd(m *mission.Mission, resume bool) string {
	promptFile := m.Dir() + "/orchestrator-prompt.md"
	mcpFile := m.Dir() + "/.mcp.json"
	base := fmt.Sprintf("claude --dangerously-skip-permissions --model %s --append-system-prompt \"$(cat '%s')\" --mcp-config '%s' --strict-mcp-config",
		m.Orchestrator.Model, promptFile, mcpFile)

	fresh := fmt.Sprintf("%s --session-id %s", base, m.Orchestrator.SessionID)
	if !resume {
		return fresh
	}
	return fmt.Sprintf("%s --resume %s || %s", base, m.Orchestrator.SessionID, fresh)
}

// orchestratorDead reports whether the orc window has fallen back to a bare
// shell (claude exited). Conservative: only true when the Claude UI footer is
// absent AND a shell prompt is visible.
func orchestratorDead(session string) bool {
	out, err := tmuxutil.CapturePane(session+":orc", 15)
	if err != nil {
		return false
	}
	if strings.Contains(out, "bypass permissions") || strings.Contains(out, "esc to interrupt") {
		return false
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "➜") || strings.HasSuffix(line, "$") ||
			strings.HasSuffix(line, "%") || strings.Contains(line, "➜ ") {
			return true
		}
	}
	return false
}

func pickMission(oxHome string) error {
	missions, err := mission.List(oxHome)
	if err != nil {
		return err
	}
	var open []*mission.Mission
	for _, m := range missions {
		if m.Open() {
			open = append(open, m)
		}
	}
	if len(open) == 0 {
		fmt.Println("No open missions.")
		fmt.Println("\nStart one:  ox go <task-ref>   ·   ox go \"a goal\" [--playbook debug]")
		return nil
	}

	fmt.Println("Open missions:")
	for i, m := range open {
		task := ""
		if m.Yoke != nil {
			task = fmt.Sprintf(" (task #%d)", m.Yoke.Seq)
		}
		fmt.Printf("  %d. %s [%s/%s]%s — %s\n", i+1, m.ID, m.Type, m.Phase, task, m.Goal)
	}
	fmt.Print("\nResume which? ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return nil
	}
	choice, err := strconv.Atoi(strings.TrimSpace(sc.Text()))
	if err != nil || choice < 1 || choice > len(open) {
		return fmt.Errorf("invalid choice")
	}
	return launchMission(oxHome, open[choice-1], true)
}

func orchestratorModel() string {
	cfg := requireConfig()
	if cfg.Multi.CaptainModel != "" {
		return cfg.Multi.CaptainModel
	}
	return "opus"
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func init() {
	goCmd.Flags().StringVar(&goPlaybook, "playbook", "task", "Mission type (task, debug, or a custom playbook)")
	goCmd.Flags().StringVar(&goModel, "model", "", "Orchestrator model override")
	goCmd.Flags().BoolVar(&goNoAttach, "no-attach", false, "Create/resume without attaching")
	rootCmd.AddCommand(goCmd)
}
