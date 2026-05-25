package review

import (
	"bufio"
	"fmt"
	"strings"
)

// HunkRange is a closed interval [Start, End] of new-file line numbers
// covered by a single diff hunk on a single file.
type HunkRange struct {
	Start int
	End   int
}

// DiffMap is the set of valid review-comment anchors per file, derived from
// a unified diff. A finding's (file, line, endLine) is postable to GitHub
// iff (a) the file appears in the map and (b) the line range falls fully
// inside one hunk for that file.
type DiffMap map[string][]HunkRange

// ParseDiff walks a unified-diff string and builds a DiffMap of valid
// anchor ranges per file. Both context lines and added lines inside a
// hunk window are considered valid (matching GitHub's review-comment
// acceptance rules — they accept comments on any line in the hunk window).
//
// Files added wholesale (`@@ -0,0 +1,N @@`) get one big hunk 1..N.
// Renames / file-mode-only changes without hunks are skipped (no anchors
// exist anyway).
func ParseDiff(diff string) DiffMap {
	out := DiffMap{}
	var currentFile string

	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			currentFile = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "+++ /dev/null"):
			currentFile = ""
		case strings.HasPrefix(line, "@@") && currentFile != "":
			hr, ok := parseHunkHeader(line)
			if ok {
				out[currentFile] = append(out[currentFile], hr)
			}
		}
	}
	return out
}

// parseHunkHeader pulls the new-file range out of a "@@ -a,b +c,d @@ ..."
// header. Returns Start = c, End = c+d-1 (so End is inclusive). When d
// is omitted in the header (single-line hunk: "+c"), d defaults to 1.
// An empty hunk (d == 0) means no new lines — returns ok=false so the
// caller can skip it.
func parseHunkHeader(s string) (HunkRange, bool) {
	// Header shape: @@ -<oldStart>[,<oldCount>] +<newStart>[,<newCount>] @@ ...
	plus := strings.Index(s, "+")
	if plus < 0 {
		return HunkRange{}, false
	}
	tail := s[plus+1:]
	// Cut at the next space (before the closing "@@").
	if sp := strings.Index(tail, " "); sp >= 0 {
		tail = tail[:sp]
	}
	// tail is now "newStart" or "newStart,newCount"
	startStr, countStr, hasCount := strings.Cut(tail, ",")
	var start, count int
	if _, err := fmt.Sscanf(startStr, "%d", &start); err != nil {
		return HunkRange{}, false
	}
	if hasCount {
		if _, err := fmt.Sscanf(countStr, "%d", &count); err != nil {
			return HunkRange{}, false
		}
	} else {
		count = 1
	}
	if count == 0 {
		return HunkRange{}, false
	}
	return HunkRange{Start: start, End: start + count - 1}, true
}

// FindingValid reports whether the finding can be posted as an inline
// review comment given the diff. line + endLine must fall within ONE
// hunk for the file (GitHub doesn't allow multi-hunk-spanning comments).
func (m DiffMap) FindingValid(f Finding) bool {
	hunks, ok := m[f.File]
	if !ok {
		return false
	}
	endLine := f.EndLine
	if endLine < f.Line {
		endLine = f.Line
	}
	for _, h := range hunks {
		if f.Line >= h.Start && endLine <= h.End {
			return true
		}
	}
	return false
}

// FilterFindings partitions findings into (postable, dropped) based on
// whether each finding's anchor lands in a valid diff hunk. Dropped
// findings should not be sent to GitHub; the caller surfaces them in the
// review's global body (or a stderr warning) so the substance isn't lost.
func (m DiffMap) FilterFindings(findings []Finding) (postable, dropped []Finding) {
	for _, f := range findings {
		if m.FindingValid(f) {
			postable = append(postable, f)
		} else {
			dropped = append(dropped, f)
		}
	}
	return postable, dropped
}
