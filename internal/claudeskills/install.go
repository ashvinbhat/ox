// Package claudeskills ships ox's Claude Code skills and installs them into
// ~/.claude/skills, where every thread picks them up.
package claudeskills

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed */SKILL.md
var files embed.FS

// marker is in every ox skill's frontmatter. A skill dir whose SKILL.md lacks
// it was written by the user, so Install leaves it alone.
var marker = []byte("owner: ox")

// Install writes each embedded skill to dir/<name>/SKILL.md. It returns the
// skills written and the ones skipped because the user owns that name.
func Install(dir string) (installed, skipped []string, err error) {
	names, err := fs.Glob(files, "*/SKILL.md")
	if err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		skill := filepath.Dir(name)
		dest := filepath.Join(dir, skill, "SKILL.md")
		if existing, err := os.ReadFile(dest); err == nil && !bytes.Contains(existing, marker) {
			skipped = append(skipped, skill)
			continue
		}
		body, err := files.ReadFile(name)
		if err != nil {
			return installed, skipped, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return installed, skipped, err
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return installed, skipped, err
		}
		installed = append(installed, skill)
	}
	return installed, skipped, nil
}
