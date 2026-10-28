// Package claudeskills ships ox's Claude Code skills and the board panel and
// installs them into ~/.claude/skills, where every thread picks them up.
package claudeskills

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

//go:embed */SKILL.md ox-board/.claude-plugin/plugin.json ox-board/hooks ox-board/types
var files embed.FS

// Each ox skill's SKILL.md and each ox plugin's manifest carries a marker. A
// folder whose file lacks it was written by the user, so Install leaves it
// alone.
var (
	skillMarker  = []byte("owner: ox")
	pluginMarker = []byte(`"author": { "name": "ox" }`)
)

// Install writes each embedded skill and plugin to dir/<name>. It returns the
// names written and the ones skipped because the user owns that name.
func Install(dir string) (installed, skipped []string, err error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		owned, marker := path.Join(name, "SKILL.md"), skillMarker
		if _, err := fs.Stat(files, owned); err != nil {
			owned, marker = path.Join(name, ".claude-plugin", "plugin.json"), pluginMarker
		}
		if existing, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(owned))); err == nil && !bytes.Contains(existing, marker) {
			skipped = append(skipped, name)
			continue
		}
		if err := copyTree(name, dir); err != nil {
			return installed, skipped, err
		}
		installed = append(installed, name)
	}
	return installed, skipped, nil
}

func copyTree(name, dir string) error {
	return fs.WalkDir(files, name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, body, 0o644)
	})
}
