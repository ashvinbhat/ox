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

	installed, skipped, _, err := Install(dir)
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

	installed, _, _, err = Install(dir)
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
	_, skipped, _, err := Install(dir)
	if err != nil || strings.Join(skipped, ",") != "ox-board" {
		t.Fatalf("skipped %v, %v", skipped, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ox-board", "hooks")); !os.IsNotExist(err) {
		t.Fatal("wrote into the user's plugin")
	}
}

func TestInstallRemovesOxsCopyOfARetiredSkillOnly(t *testing.T) {
	write := func(dir, body string) {
		skill := filepath.Join(dir, "daily-archive", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ours, theirs := t.TempDir(), t.TempDir()
	write(ours, "---\nname: daily-archive\nowner: ox\n---\n")
	write(theirs, "---\nname: daily-archive\n---\nmine")

	_, _, removed, err := Install(ours)
	if err != nil || strings.Join(removed, ",") != "daily-archive" {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(ours, "daily-archive")); !os.IsNotExist(err) {
		t.Fatal("ox's copy of a retired skill is still installed")
	}
	if _, _, removed, _ := Install(theirs); len(removed) != 0 {
		t.Fatalf("removed the user's skill: %v", removed)
	}
	if _, err := os.Stat(filepath.Join(theirs, "daily-archive", "SKILL.md")); err != nil {
		t.Fatal("the user's skill under a retired name is gone")
	}
}
