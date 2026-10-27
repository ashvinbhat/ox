package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRetiresOldBuiltInsAndKeepsCustomHooks(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ox-instructions.sh", "yoke-ready-tasks.sh", "workspace-context.sh", "my-own.sh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := NewManager(home)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, h := range m.List() {
		names = append(names, h.Name)
	}
	if len(names) != 2 || names[0] != "my-own" || names[1] != "ox-brief" {
		t.Fatalf("hooks after Init = %v, want [my-own ox-brief]", names)
	}
	info, err := os.Stat(filepath.Join(dir, "ox-brief.sh"))
	if err != nil || info.Mode()&0o100 == 0 {
		t.Fatalf("ox-brief.sh missing or not executable: %v", err)
	}
}
