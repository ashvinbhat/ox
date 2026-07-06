package ghreview

import "strings"

// Dedupe merges findings that overlap on (file, line, category) across
// agents. When two findings target the same anchor with the same category,
// we keep the highest severity, concatenate distinct agent attributions,
// and union the bodies (longer body wins, shorter body appended as a "Also:"
// line if it adds detail).
//
// Findings on the same line but in different categories (e.g. a correctness
// bug and a design concern at the same statement) are preserved separately
// so the author sees both perspectives.
func Dedupe(findings []Finding) []Finding {
	if len(findings) <= 1 {
		return findings
	}

	type key struct {
		file string
		line int
		cat  Category
	}
	groups := map[key][]Finding{}
	order := []key{}

	for _, f := range findings {
		k := key{file: f.File, line: f.Line, cat: f.Category}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}

	out := make([]Finding, 0, len(order))
	for _, k := range order {
		out = append(out, mergeGroup(groups[k]))
	}
	return out
}

func mergeGroup(group []Finding) Finding {
	if len(group) == 1 {
		return group[0]
	}

	// Pick the finding with the worst severity as the base.
	base := group[0]
	for _, f := range group[1:] {
		if severityOrder[f.Severity] < severityOrder[base.Severity] {
			base = f
		}
	}

	// Collect distinct agent attributions in deterministic order of appearance.
	agents := []string{}
	seen := map[string]bool{}
	for _, f := range group {
		if f.Agent == "" || seen[f.Agent] {
			continue
		}
		seen[f.Agent] = true
		agents = append(agents, f.Agent)
	}
	merged := base
	merged.Agent = strings.Join(agents, "+")

	// Union the bodies: keep the longest as the primary body; append distinct
	// shorter bodies under "Also flagged by …" lines if they add detail.
	primary := base.Body
	for _, f := range group {
		if f.Body == primary || f.Body == "" {
			continue
		}
		if !strings.Contains(primary, f.Body) {
			primary += "\n\nAlso flagged by " + f.Agent + ": " + f.Body
		}
	}
	merged.Body = primary

	// If multiple titles, keep the base's title; titles are short labels and
	// concatenating them tends to make worse copy than picking one.
	return merged
}

var severityOrder = map[Severity]int{
	SeverityBlocker: 0,
	SeverityIssue:   1,
	SeveritySuggest: 2,
	SeverityNit:     3,
}

// SortBySeverity orders findings blocker-first, then by file/line.
func SortBySeverity(findings []Finding) {
	for i := 0; i < len(findings); i++ {
		for j := i + 1; j < len(findings); j++ {
			si, sj := severityOrder[findings[i].Severity], severityOrder[findings[j].Severity]
			if sj < si || (sj == si && (findings[j].File < findings[i].File ||
				(findings[j].File == findings[i].File && findings[j].Line < findings[i].Line))) {
				findings[i], findings[j] = findings[j], findings[i]
			}
		}
	}
}
