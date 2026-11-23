// Package claudeskills ships ox's Claude Code skills and installs them into
// ~/.claude/skills, where every thread picks them up.
package claudeskills

import (
	"bytes"
	"embed"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
)

//go:embed */SKILL.md
var files embed.FS

// Each ox skill's SKILL.md carries a marker. A folder whose SKILL.md lacks it
// was written by the user, so Install leaves it alone.
var skillMarker = []byte("owner: ox")

// retired are skills and plugins earlier ox versions installed. A copy left
// in place would keep running in threads. The ox-board panel went when each
// thread's brief started listing its sub-threads.
var retired = []string{"daily-archive", "ox-board"}

// Install writes each embedded skill to dir/<name> and removes ox's copy of
// each retired skill or plugin. It returns the names written, the ones
// skipped because the user owns that name, and the ones removed.
func Install(dir string) (installed, skipped, removed []string, err error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if existing, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md")); err == nil && !bytes.Contains(existing, skillMarker) {
			skipped = append(skipped, name)
			continue
		}
		if err := copySkill(name, dir); err != nil {
			return installed, skipped, removed, err
		}
		installed = append(installed, name)
	}
	for _, name := range retired {
		if !oxOwns(filepath.Join(dir, name)) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return installed, skipped, removed, err
		}
		removed = append(removed, name)
	}
	return installed, skipped, removed, nil
}

// oxOwns reports a skill whose SKILL.md carries ox's marker, or a plugin
// whose manifest names ox as its author.
func oxOwns(folder string) bool {
	if b, err := os.ReadFile(filepath.Join(folder, "SKILL.md")); err == nil {
		return bytes.Contains(b, skillMarker)
	}
	b, err := os.ReadFile(filepath.Join(folder, ".claude-plugin", "plugin.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Author struct {
			Name string `json:"name"`
		} `json:"author"`
	}
	return json.Unmarshal(b, &manifest) == nil && manifest.Author.Name == "ox"
}

func copySkill(name, dir string) error {
	body, err := files.ReadFile(path.Join(name, "SKILL.md"))
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, body, 0o644)
}
