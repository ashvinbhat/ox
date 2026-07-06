package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const repoDocMaxLines = 300

func repoDocPath(oxHome, repo string) string {
	return filepath.Join(oxHome, "memory", "repos", repo+".md")
}

// RepoDoc returns the living knowledge doc for a repo ("" when none exists).
func RepoDoc(oxHome, repo string) string {
	data, err := os.ReadFile(repoDocPath(oxHome, repo))
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteRepoDoc replaces a repo's knowledge doc (full-rewrite discipline: no
// append path exists, so stale lines must survive an active rewrite to
// persist). The previous revision is kept in .history (last 10).
func WriteRepoDoc(oxHome, repo, content, updatedBy string) error {
	if n := strings.Count(content, "\n") + 1; n > repoDocMaxLines {
		return fmt.Errorf("repo doc for %s is %d lines (cap %d) — condense it", repo, n, repoDocMaxLines)
	}

	dir := filepath.Dir(repoDocPath(oxHome, repo))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := repoDocPath(oxHome, repo)
	revision := 1
	if old, err := os.ReadFile(path); err == nil {
		revision = parseRevision(string(old)) + 1
		histDir := filepath.Join(dir, ".history")
		os.MkdirAll(histDir, 0o755)
		os.WriteFile(filepath.Join(histDir, fmt.Sprintf("%s.%d.md", repo, revision-1)), old, 0o644)
		pruneHistory(histDir, repo, 10)
	}

	header := fmt.Sprintf("---\nrepo: %s\nupdated_at: %s\nupdated_by: %s\nrevision: %d\n---\n",
		repo, time.Now().Format(time.RFC3339), updatedBy, revision)
	body := stripFrontmatter(content)
	return os.WriteFile(path, []byte(header+body), 0o644)
}

func parseRevision(doc string) int {
	for _, line := range strings.Split(doc, "\n") {
		if rev, ok := strings.CutPrefix(line, "revision: "); ok {
			var n int
			fmt.Sscanf(rev, "%d", &n)
			return n
		}
		if line == "---" && !strings.HasPrefix(doc, "---") {
			break
		}
	}
	return 0
}

func stripFrontmatter(s string) string {
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[4:]
	if i := strings.Index(rest, "\n---\n"); i >= 0 {
		return rest[i+5:]
	}
	return s
}

func pruneHistory(histDir, repo string, keep int) {
	entries, err := os.ReadDir(histDir)
	if err != nil {
		return
	}
	var revs []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), repo+".") {
			revs = append(revs, e.Name())
		}
	}
	if len(revs) <= keep {
		return
	}
	// Names sort by revision because of the numeric suffix pattern; delete oldest.
	for i := 0; i < len(revs); i++ {
		for j := i + 1; j < len(revs); j++ {
			if revLess(revs[j], revs[i]) {
				revs[i], revs[j] = revs[j], revs[i]
			}
		}
	}
	for _, name := range revs[:len(revs)-keep] {
		os.Remove(filepath.Join(histDir, name))
	}
}

func revLess(a, b string) bool {
	na, nb := revNum(a), revNum(b)
	return na < nb
}

func revNum(name string) int {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		return 0
	}
	var n int
	fmt.Sscanf(parts[len(parts)-2], "%d", &n)
	return n
}
