package review

import (
	"fmt"
	"io"
	"sort"
)

var severityOrder = map[Severity]int{
	SeverityBlocker: 0,
	SeverityIssue:   1,
	SeveritySuggest: 2,
	SeverityNit:     3,
}

// Render prints findings to the writer, grouped by severity, ordered
// blocker → issue → suggest → nit, then by file:line within each group.
func Render(w io.Writer, findings []Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, "No findings. ✓")
		return
	}

	sorted := append([]Finding(nil), findings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := severityOrder[sorted[i].Severity], severityOrder[sorted[j].Severity]
		if si != sj {
			return si < sj
		}
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		return sorted[i].Line < sorted[j].Line
	})

	// Counts per severity.
	counts := map[Severity]int{}
	for _, f := range sorted {
		counts[f.Severity]++
	}
	fmt.Fprintf(w, "\nFindings: %d blocker · %d issue · %d suggest · %d nit\n\n",
		counts[SeverityBlocker], counts[SeverityIssue], counts[SeveritySuggest], counts[SeverityNit])

	currentSev := Severity("")
	for i, f := range sorted {
		if f.Severity != currentSev {
			currentSev = f.Severity
			fmt.Fprintf(w, "── %s ──\n", string(f.Severity))
		}
		lineSpan := fmt.Sprintf("%d", f.Line)
		if f.EndLine > f.Line {
			lineSpan = fmt.Sprintf("%d-%d", f.Line, f.EndLine)
		}
		fmt.Fprintf(w, "[%d] %s:%s  (%s · %s)\n    %s\n",
			i+1, f.File, lineSpan, f.Category, f.Agent, f.Title)
		if f.Body != "" {
			for _, line := range splitLines(f.Body) {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
		fmt.Fprintln(w)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
