package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RuleSource is one rule blob plus its provenance (so we can header it in
// the review context and the user can tell which file produced which advice).
type RuleSource struct {
	Origin  string // e.g. "global", "repo", "--rules", or a path
	Content string
}

// DiscoverRules returns review-rule blobs in precedence order: lowest
// precedence first, highest last (so later layers override / extend earlier
// ones in the agent's reading).
//
// Layers (lowest → highest):
//  1. Global:  ~/<oxHome>/review-rules.md
//  2. Per-repo: <worktreePath>/.ox/review-rules.md
//  3. Explicit: each path in extraPaths, in order
//
// Missing files are silently skipped. Unreadable files (permission, etc.)
// return an error so the caller knows something is off.
func DiscoverRules(oxHome, worktreePath string, extraPaths []string) ([]RuleSource, error) {
	var out []RuleSource

	// 1. Global.
	globalPath := filepath.Join(oxHome, "review-rules.md")
	if rs, err := loadRule(globalPath, "global"); err != nil {
		return nil, err
	} else if rs != nil {
		out = append(out, *rs)
	}

	// 2. Per-repo (lives in the worktree, so PR-versioned along with the code).
	repoPath := filepath.Join(worktreePath, ".ox", "review-rules.md")
	if rs, err := loadRule(repoPath, "repo"); err != nil {
		return nil, err
	} else if rs != nil {
		out = append(out, *rs)
	}

	// 3. Explicit --rules paths.
	for _, p := range extraPaths {
		if p == "" {
			continue
		}
		rs, err := loadRule(p, "--rules "+p)
		if err != nil {
			return nil, err
		}
		if rs == nil {
			return nil, fmt.Errorf("--rules file %q not found", p)
		}
		out = append(out, *rs)
	}

	return out, nil
}

// FormatForContext converts the discovered rule sources into the []string
// shape that PrepareWorkspace expects, each blob prefixed with a header
// identifying its origin.
func FormatForContext(sources []RuleSource) []string {
	if len(sources) == 0 {
		return nil
	}
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		var sb strings.Builder
		fmt.Fprintf(&sb, "### Rules from %s\n\n", s.Origin)
		sb.WriteString(strings.TrimRight(s.Content, "\n"))
		out = append(out, sb.String())
	}
	return out
}

func loadRule(path, origin string) (*RuleSource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rules %s: %w", path, err)
	}
	content := strings.TrimSpace(string(b))
	if content == "" {
		return nil, nil
	}
	return &RuleSource{Origin: origin, Content: content}, nil
}
