// Package tracks maps work onto the user's workstreams, kept in
// ~/.ox/tracks.yaml. Order in the file is priority: the first track whose
// match terms fit a thread wins.
package tracks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Track is one workstream.
type Track struct {
	Name  string   `yaml:"name"`
	Short string   `yaml:"short,omitempty"`
	Goal  string   `yaml:"goal,omitempty"`
	Match []string `yaml:"match"`
}

type file struct {
	Tracks []Track `yaml:"tracks"`
}

// Path is where tracks.yaml lives under OX_HOME.
func Path(oxHome string) string {
	return filepath.Join(oxHome, "tracks.yaml")
}

// Load reads tracks.yaml. A missing file means no tracks yet.
func Load(oxHome string) ([]Track, error) {
	b, err := os.ReadFile(Path(oxHome))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(oxHome), err)
	}
	return f.Tracks, nil
}

// Label is the short column name ox shows for the track.
func (t Track) Label() string {
	if t.Short != "" {
		return t.Short
	}
	if fields := strings.Fields(t.Name); len(fields) > 0 {
		return strings.ToLower(fields[0])
	}
	return ""
}

// Of returns the first track with a match term starting a word,
// case-insensitively, in any of texts, or nil when none fits. Anchoring at a
// word start keeps a short term like "ox" from matching "inbox".
func Of(tracks []Track, texts ...string) *Track {
	hay := strings.ToLower(strings.Join(texts, "\n"))
	for i := range tracks {
		for _, term := range tracks[i].Match {
			if term != "" && startsWord(hay, strings.ToLower(term)) {
				return &tracks[i]
			}
		}
	}
	return nil
}

func startsWord(hay, term string) bool {
	for from := 0; from < len(hay); {
		i := strings.Index(hay[from:], term)
		if i < 0 {
			return false
		}
		at := from + i
		if at == 0 || !isWordByte(hay[at-1]) {
			return true
		}
		from = at + 1
	}
	return false
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}
