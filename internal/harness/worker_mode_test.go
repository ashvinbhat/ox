package harness

import (
	"strings"
	"testing"

	"github.com/ashvinbhat/ox/internal/mission"
)

// The pane id's presence is the per-worker mode flag. This guards the
// backward-compat guarantee: a worker with no pane id behaves exactly as
// before (session mode), so existing/in-flight missions are unaffected.
func TestWorkerModeAndTarget(t *testing.T) {
	sessionWorker := &Worker{ID: "w1", TmuxSession: "ox-m1-w1"}
	if sessionWorker.Paned() {
		t.Error("worker with no pane id must be session-mode")
	}
	if got := sessionWorker.Target(); got != "ox-m1-w1" {
		t.Errorf("session target = %q, want the session name", got)
	}

	panedWorker := &Worker{ID: "w2", TmuxSession: "ox-m1-w2", TmuxPane: "%7"}
	if !panedWorker.Paned() {
		t.Error("worker with a pane id must be pane-mode")
	}
	if got := panedWorker.Target(); got != "%7" {
		t.Errorf("pane target = %q, want the pane id", got)
	}
}

func TestWorkerEngine(t *testing.T) {
	claude := &Worker{ID: "w1", Model: "sonnet"}
	if claude.UsesOpencode() {
		t.Error("no engine set must be claude (default)")
	}
	oc := &Worker{ID: "w2", Model: "openrouter/stealth/ox-alpha", Engine: "opencode"}
	if !oc.UsesOpencode() {
		t.Error("engine=opencode must route to opencode")
	}
	m := &mission.Mission{}
	if cmd := workerClaudeCmd(m, oc, "sid", ""); !strings.Contains(cmd, "exec opencode") {
		t.Errorf("opencode worker launch cmd = %q, want it to exec opencode", cmd)
	}
	if cmd := workerClaudeCmd(m, claude, "sid", ""); cmd == "opencode" {
		t.Error("claude worker must not launch opencode")
	}
}
