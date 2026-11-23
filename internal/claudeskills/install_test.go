package claudeskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skills = "memory-check,open-pr,own-it,park,recap,review-pr,tidy"

func TestInstallWritesOxSkillsAndLeavesUserSkillsAlone(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "review-asks", "SKILL.md")
	write(t, user, "---\nname: review-asks\n---\nmine")

	installed, skipped, _, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(installed, ",") != skills || strings.Join(skipped, ",") != "review-asks" {
		t.Fatalf("installed %v, skipped %v", installed, skipped)
	}
	if b, _ := os.ReadFile(user); string(b) != "---\nname: review-asks\n---\nmine" {
		t.Fatal("user's skill was overwritten")
	}

	installed, _, _, err = Install(dir)
	if err != nil || strings.Join(installed, ",") != skills {
		t.Fatalf("re-install of an ox skill: %v, %v", installed, err)
	}
}

func TestInstallRemovesOxsCopyOfARetiredSkillOnly(t *testing.T) {
	ours, theirs := t.TempDir(), t.TempDir()
	write(t, filepath.Join(ours, "daily-archive", "SKILL.md"), "---\nname: daily-archive\nowner: ox\n---\n")
	write(t, filepath.Join(theirs, "daily-archive", "SKILL.md"), "---\nname: daily-archive\n---\nmine")

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

func TestInstallRetiresOxsBoardPluginOnly(t *testing.T) {
	ours, theirs := t.TempDir(), t.TempDir()
	write(t, filepath.Join(ours, "ox-board", ".claude-plugin", "plugin.json"), `{"name": "ox-board", "author": {"name": "ox"}}`)
	write(t, filepath.Join(ours, "ox-board", "hooks", "register.tsx"), "export {}")
	write(t, filepath.Join(theirs, "ox-board", ".claude-plugin", "plugin.json"), `{"name": "ox-board", "author": {"name": "me"}}`)

	_, _, removed, err := Install(ours)
	if err != nil || strings.Join(removed, ",") != "ox-board" {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(ours, "ox-board")); !os.IsNotExist(err) {
		t.Fatal("ox's board plugin is still installed")
	}
	if _, _, removed, _ := Install(theirs); len(removed) != 0 {
		t.Fatalf("removed the user's plugin: %v", removed)
	}
	if _, err := os.Stat(filepath.Join(theirs, "ox-board", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("the user's plugin under a retired name is gone")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
