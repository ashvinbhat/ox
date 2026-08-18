package mission

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAdoptsTaskSeq(t *testing.T) {
	home := t.TempDir()

	m, err := Create(home, "task", "tags endpoint", &YokeRef{ID: "abcd", Seq: 139}, "opus", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "m139" {
		t.Errorf("task-backed ID = %s, want m139", m.ID)
	}

	// Same task again (reopened after close) → lettered suffix, no collision.
	m2, err := Create(home, "task", "tags endpoint round two", &YokeRef{ID: "abcd", Seq: 139}, "opus", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if m2.ID != "m139b" {
		t.Errorf("reopened ID = %s, want m139b", m2.ID)
	}

	// Ad-hoc missions draw from the counter.
	adhoc, err := Create(home, "task", "freeform exploration", nil, "opus", "s3")
	if err != nil {
		t.Fatal(err)
	}
	if adhoc.ID != "m1" {
		t.Errorf("adhoc ID = %s, want m1", adhoc.ID)
	}

	// Counter landing on a task-claimed id skips past it.
	if err := os.WriteFile(filepath.Join(Root(home), ".counter"), []byte("138"), 0o644); err != nil {
		t.Fatal(err)
	}
	skip, err := Create(home, "task", "counter collision", nil, "opus", "s4")
	if err != nil {
		t.Fatal(err)
	}
	if skip.ID != "m140" {
		t.Errorf("collision ID = %s, want m140 (skipping taken m139)", skip.ID)
	}
}

func TestReopenRestoresPriorPhase(t *testing.T) {
	home := t.TempDir()
	m, err := Create(home, "task", "reopen me", &YokeRef{ID: "z1", Seq: 55}, "opus", "s1")
	if err != nil {
		t.Fatal(err)
	}
	m.SetPhase("executing", "test")
	m.SetPhase("shipping", "test")
	m.SetPhase(PhaseClosed, "test")
	m.Outcome = "shipped"
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if m.Open() {
		t.Fatal("expected closed before reopen")
	}

	re, err := Reopen(home, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !re.Open() {
		t.Error("mission should be open after reopen")
	}
	if re.Phase != "shipping" {
		t.Errorf("phase = %s, want shipping (last non-closed)", re.Phase)
	}
	if re.Outcome != "" {
		t.Errorf("outcome should be cleared, got %q", re.Outcome)
	}
}
