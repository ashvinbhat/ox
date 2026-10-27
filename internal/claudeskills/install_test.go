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
	if strings.Join(installed, ",") != "review-pr" || strings.Join(skipped, ",") != "review-asks" {
		t.Fatalf("installed %v, skipped %v", installed, skipped)
	}
	if b, _ := os.ReadFile(user); string(b) != "---\nname: review-asks\n---\nmine" {
		t.Fatal("user's skill was overwritten")
	}

	installed, _, err = Install(dir)
	if err != nil || strings.Join(installed, ",") != "review-pr" {
		t.Fatalf("re-install of an ox skill: %v, %v", installed, err)
	}
}
