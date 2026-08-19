package tmuxutil

import "testing"

// Exercises the pane-layout primitives against a real tmux server. Skips when
// tmux is unavailable (CI without tmux).
func TestAgentPaneLifecycle(t *testing.T) {
	if !IsAvailable() {
		t.Skip("tmux not available")
	}
	sess := "ox-panetest"
	KillSession(sess)
	if err := NewSession(sess, "/tmp"); err != nil {
		t.Fatal(err)
	}
	defer KillSession(sess)
	RenameWindow(sess, "orc")

	p1, err := EnsureAgentPane(sess, "agents", "/tmp") // creates the window
	if err != nil {
		t.Fatal(err)
	}
	p2, err := EnsureAgentPane(sess, "agents", "/tmp") // splits + tiles
	if err != nil {
		t.Fatal(err)
	}
	p3, err := EnsureAgentPane(sess, "agents", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	SetPaneTitle(p1, "builder")

	if !HasWindow(sess, "orc") {
		t.Error("orc window must stay separate from agents")
	}
	if !HasWindow(sess, "agents") {
		t.Error("agents window should exist")
	}
	for _, p := range []string{p1, p2, p3} {
		if !PaneAlive(p) {
			t.Errorf("pane %s should be alive", p)
		}
	}
	if PaneAlive("%99999") {
		t.Error("bogus pane must not be alive")
	}

	// Reaping one pane leaves the siblings and the window intact.
	if err := KillPane(p2); err != nil {
		t.Fatal(err)
	}
	if PaneAlive(p2) {
		t.Error("killed pane should be gone")
	}
	if !PaneAlive(p1) || !PaneAlive(p3) {
		t.Error("sibling panes must survive a single kill")
	}
	if !HasWindow(sess, "agents") {
		t.Error("agents window should survive while a pane remains")
	}
}
