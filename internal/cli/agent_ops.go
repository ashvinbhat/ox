package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/ashvinbhat/ox/internal/agent"
	"github.com/ashvinbhat/ox/internal/tmuxutil"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach <agent-id>",
	Short: "Attach to an agent's tmux session",
	Long: `Takes over an agent's tmux session interactively.
Detach with Ctrl-B then D to return to your terminal.

Examples:
  ox attach auth-api`,
	Args: cobra.ExactArgs(1),
	RunE: runAttach,
}

var peekCmd = &cobra.Command{
	Use:   "peek <agent-id>",
	Short: "View an agent's recent terminal output",
	Long: `Captures and displays the last 50 lines from an agent's tmux session.

Examples:
  ox peek auth-api`,
	Args: cobra.ExactArgs(1),
	RunE: runPeek,
}

var killCmd = &cobra.Command{
	Use:   "kill <agent-id>",
	Short: "Terminate an agent",
	Long: `Kills an agent's tmux session and updates its status to killed.

Examples:
  ox kill auth-api`,
	Args: cobra.ExactArgs(1),
	RunE: runKill,
}

var respawnCmd = &cobra.Command{
	Use:   "respawn <agent-id>",
	Short: "Restart a dead agent in its existing worktree",
	Long: `Recreates the tmux session and launches Claude Code for an agent whose
session died. The worktree, branch, and all commits are preserved —
Claude will see the existing work and continue from where it left off.

Examples:
  ox respawn auth-api
  ox respawn backend-data-model`,
	Args: cobra.ExactArgs(1),
	RunE: runRespawn,
}

var msgCmd = &cobra.Command{
	Use:   "msg <agent-id> <message>",
	Short: "Send a message to an agent",
	Long: `Types a message into an agent's Claude Code session via tmux send-keys.

Examples:
  ox msg auth-api "focus on the middleware first"
  ox msg auth-api "stop and commit what you have"`,
	Args: cobra.MinimumNArgs(2),
	RunE: runMsg,
}

func runAttach(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	a, _, err := mgr.FindAgent(args[0])
	if err != nil {
		return err
	}

	if !tmuxutil.HasSession(a.TmuxSession) {
		return fmt.Errorf("agent %q session is not running", a.ID)
	}

	fmt.Printf("Attaching to %s... (Ctrl-B then D to detach)\n", a.TmuxSession)
	return tmuxutil.Attach(a.TmuxSession)
}

func runPeek(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	a, _, err := mgr.FindAgent(args[0])
	if err != nil {
		return err
	}

	if !tmuxutil.HasSession(a.TmuxSession) {
		return fmt.Errorf("agent %q session is not running (status: %s)", a.ID, a.Status)
	}

	output, err := tmuxutil.CapturePane(a.TmuxSession, 50)
	if err != nil {
		return fmt.Errorf("capture pane: %w", err)
	}

	fmt.Printf("── %s (%s) ──\n", a.ID, a.TmuxSession)
	fmt.Print(output)
	fmt.Printf("── end ──\n")

	return nil
}

func runKill(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	a, taskID, err := mgr.FindAgent(args[0])
	if err != nil {
		return err
	}

	if tmuxutil.HasSession(a.TmuxSession) {
		if err := tmuxutil.KillSession(a.TmuxSession); err != nil {
			return fmt.Errorf("kill session: %w", err)
		}
	}

	err = mgr.UpdateAgent(taskID, a.ID, func(a *agent.Agent) {
		now := time.Now()
		a.Status = agent.StatusKilled
		a.FinishedAt = &now
	})
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Printf("Killed agent %q\n", a.ID)
	return nil
}

func runRespawn(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	a, taskID, err := mgr.FindAgent(args[0])
	if err != nil {
		return err
	}

	if tmuxutil.HasSession(a.TmuxSession) {
		return fmt.Errorf("agent %q is already running — use 'ox attach %s' instead", a.ID, a.ID)
	}

	fmt.Printf("Respawning agent %q in existing worktree...\n", a.ID)
	fmt.Printf("  Worktree: %s\n", a.WorktreePath)
	fmt.Printf("  Branch: %s\n", a.BranchName)

	if err := mgr.RespawnAgent(taskID, a); err != nil {
		return fmt.Errorf("respawn: %w", err)
	}

	fmt.Printf("\nAgent respawned: %s\n", a.TmuxSession)
	fmt.Println("Claude will see existing commits and continue from where it left off.")
	fmt.Printf("\n  ox peek %s      # view output\n", a.ID)
	fmt.Printf("  ox attach %s    # take over session\n", a.ID)

	return nil
}

func runMsg(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	agentID := args[0]
	message := strings.Join(args[1:], " ")

	a, _, err := mgr.FindAgent(agentID)
	if err != nil {
		return err
	}

	if !tmuxutil.HasSession(a.TmuxSession) {
		return fmt.Errorf("agent %q session is not running", a.ID)
	}

	if err := tmuxutil.SendKeys(a.TmuxSession, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	fmt.Printf("Sent to %s: %s\n", a.ID, message)
	return nil
}

var btwCmd = &cobra.Command{
	Use:   "btw <agent-id> <message>",
	Short: "Send background context to an agent without using a turn",
	Long: `Sends a /btw message to an agent's Claude Code session. This adds context
to the conversation without triggering a response or consuming a turn.

Use this to give agents information they should know without interrupting
their current work.

Examples:
  ox btw auth-api "the auth table uses uuid not int for user_id"
  ox btw backend-data-model "use Lombok @Data instead of manual getters"`,
	Args: cobra.MinimumNArgs(2),
	RunE: runBtw,
}

func runBtw(cmd *cobra.Command, args []string) error {
	cfg := requireConfig()
	mgr := agent.NewManager(cfg.Home, cfg)

	agentID := args[0]
	message := strings.Join(args[1:], " ")

	a, _, err := mgr.FindAgent(agentID)
	if err != nil {
		return err
	}

	if !tmuxutil.HasSession(a.TmuxSession) {
		return fmt.Errorf("agent %q session is not running", a.ID)
	}

	btwMsg := "/btw " + message
	if err := tmuxutil.SendKeys(a.TmuxSession, btwMsg); err != nil {
		return fmt.Errorf("send btw: %w", err)
	}

	fmt.Printf("Sent /btw to %s: %s\n", a.ID, message)
	return nil
}

func init() {
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(peekCmd)
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(msgCmd)
	rootCmd.AddCommand(respawnCmd)
	rootCmd.AddCommand(btwCmd)
}
