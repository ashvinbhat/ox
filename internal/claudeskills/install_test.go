package claudeskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWritesOxSkillsAndLeavesUserSkillsAlone(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "review-asks", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("---\nname: review-asks\n---\nmine"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed, skipped, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(installed, ",") != "memory-check,open-pr,own-it,ox-board,park,recap,review-pr,tidy" || strings.Join(skipped, ",") != "review-asks" {
		t.Fatalf("installed %v, skipped %v", installed, skipped)
	}
	if b, _ := os.ReadFile(user); string(b) != "---\nname: review-asks\n---\nmine" {
		t.Fatal("user's skill was overwritten")
	}
	for _, f := range []string{"ox-board/.claude-plugin/plugin.json", "ox-board/hooks/hooks.json", "ox-board/hooks/register.tsx", "ox-board/types/index.d.ts"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("board panel: %v", err)
		}
	}

	installed, _, err = Install(dir)
	if err != nil || strings.Join(installed, ",") != "memory-check,open-pr,own-it,ox-board,park,recap,review-pr,tidy" {
		t.Fatalf("re-install of an ox skill: %v, %v", installed, err)
	}
}

func TestInstallLeavesAPluginOfTheUsersAlone(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "ox-board", ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name": "ox-board"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, skipped, err := Install(dir)
	if err != nil || strings.Join(skipped, ",") != "ox-board" {
		t.Fatalf("skipped %v, %v", skipped, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ox-board", "hooks")); !os.IsNotExist(err) {
		t.Fatal("wrote into the user's plugin")
	}
}
