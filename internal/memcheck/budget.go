package memcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// KindBudget is the words every thread loads before its own work, over
// WordBudget.
const KindBudget = "budget"

// WordBudget is how many words of CLAUDE.md and MEMORY.md a thread may load
// at start. Past it, each rule competes with the rest for attention.
const WordBudget = 3000

// AlwaysLoaded is what Claude Code reads into every thread of the store's
// project: the user CLAUDE.md, each CLAUDE.md and CLAUDE.local.md from the
// project folder up to /, and the store's MEMORY.md. cwds are folders
// threads run in; the project is the one Claude Code named the store after.
func AlwaysLoaded(claudeHome, store string, cwds []string) []string {
	candidates := []string{filepath.Join(claudeHome, "CLAUDE.md")}
	if dir := projectDir(store, cwds); dir != "" {
		d := dir
		for range strings.Count(dir, string(filepath.Separator)) + 1 {
			candidates = append(candidates, filepath.Join(d, "CLAUDE.md"), filepath.Join(d, "CLAUDE.local.md"))
			d = filepath.Dir(d)
		}
	}
	candidates = append(candidates, filepath.Join(store, IndexFile))
	var out []string
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

var notAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// projectDir is the folder whose name, as Claude Code writes it under
// projects/ (every non-alphanumeric rune a dash), is the store's parent.
func projectDir(store string, cwds []string) string {
	want := filepath.Base(filepath.Dir(store))
	for _, c := range cwds {
		if c = filepath.Clean(c); notAlnum.ReplaceAllString(c, "-") == want {
			return c
		}
	}
	return ""
}

type section struct {
	where string
	words int
}

// budget counts the words in files, MEMORY.md only up to the lines Claude
// Code reads, and names the largest sections when the total is over limit.
func budget(files []string, limit int, label func(string) string) []Suggestion {
	total := 0
	var perFile []string
	var sections []section
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		text := string(data)
		if filepath.Base(f) == IndexFile {
			text = strings.Join(firstLines(text, IndexLimit), "\n")
		}
		n := 0
		cur := section{where: label(f)}
		for line := range strings.SplitSeq(text, "\n") {
			if h, ok := headingOf(line); ok {
				if cur.words > 0 {
					sections = append(sections, cur)
				}
				cur = section{where: label(f) + " › " + h}
			}
			w := len(strings.Fields(line))
			cur.words += w
			n += w
		}
		if cur.words > 0 {
			sections = append(sections, cur)
		}
		total += n
		perFile = append(perFile, fmt.Sprintf("%s %d", label(f), n))
	}
	if total <= limit {
		return nil
	}
	sort.SliceStable(sections, func(i, j int) bool { return sections[i].words > sections[j].words })
	var top []string
	for _, s := range sections[:min(3, len(sections))] {
		top = append(top, fmt.Sprintf("%s %d", s.where, s.words))
	}
	detail := fmt.Sprintf("every thread loads %d words, %d over the %d budget (%s); largest: %s",
		total, total-limit, limit, strings.Join(perFile, ", "), strings.Join(top, ", "))
	return []Suggestion{{ID: KindBudget, Kind: KindBudget, File: "always-loaded", Detail: detail}}
}

func headingOf(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, "#")
	if trimmed == line || !strings.HasPrefix(trimmed, " ") {
		return "", false
	}
	return strings.TrimSpace(trimmed), true
}

func firstLines(s string, n int) []string {
	lines := strings.Split(s, "\n")
	return lines[:min(n, len(lines))]
}
