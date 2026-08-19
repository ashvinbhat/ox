package harness

import "testing"

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
