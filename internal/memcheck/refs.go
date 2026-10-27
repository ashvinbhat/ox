package memcheck

import (
	"os"
	"regexp"
	"strings"
)

var (
	fenceRe = regexp.MustCompile("(?s)```.*?```")
	spanRe  = regexp.MustCompile("`([^`\n]+)`")
	// CamelCase with two humps, camelCase, or snake_case: names that read as
	// code. Single words and UPPER_SNAKE (env vars, enum values) are too
	// often external to check.
	identRe = regexp.MustCompile(`\b(?:[A-Z][a-z0-9]+(?:[A-Z][a-z0-9]*)+|[a-z][a-z0-9]*(?:[A-Z][a-z0-9]*)+|[a-z][a-z0-9]*(?:_[a-z0-9]+)+)\b`)
	// findByX, GenericResponseXDto, markSomethingFailed: examples, not code.
	placeholderRe = regexp.MustCompile(`[a-z0-9]X(?:[A-Z][a-z]|$)|Foo|Something|Example`)
	codeExtRe     = regexp.MustCompile(`\.(go|java|kt|ts|tsx|js|jsx|mjs|py|sql|proto|xml|json|ya?ml|sh|md)$`)
	fileExtRe     = regexp.MustCompile(`\.[a-z]{1,5}$`)
	absRe         = regexp.MustCompile(`(?:~|/Users)/[^\s` + "`" + `'"()\[\]<>{}*$,;|]*`)
	oxRe          = regexp.MustCompile(`(?:^|[\s;&|(])ox((?:[ \t]+[a-z][a-z0-9-]*){1,3})`)
)

// spans returns the code in a memory: fenced blocks and inline backtick spans.
func spans(text string) []string {
	var out []string
	for _, f := range fenceRe.FindAllString(text, -1) {
		out = append(out, strings.Trim(f, "`"))
	}
	for _, m := range spanRe.FindAllStringSubmatch(fenceRe.ReplaceAllString(text, " "), -1) {
		out = append(out, m[1])
	}
	return out
}

// templated reports whether a reference is a pattern, not a name.
func templated(s string) bool {
	return strings.ContainsAny(s, "<>{}*$…") || strings.Contains(s, "...")
}

// codeRefs returns the identifiers and repo-relative file paths a memory
// names in code spans.
func codeRefs(text string) (idents, paths []string) {
	seenID, seenPath := map[string]bool{}, map[string]bool{}
	for _, sp := range spans(text) {
		for _, field := range strings.Fields(sp) {
			field = strings.TrimRight(strings.TrimLeft(field, `"'(`), `"'),;:.`)
			if strings.Contains(field, "/") {
				if p := relPath(field); p != "" && !seenPath[p] {
					seenPath[p] = true
					paths = append(paths, p)
				}
				continue
			}
			if fileExtRe.MatchString(field) || templated(field) {
				continue
			}
			for _, id := range identRe.FindAllString(field, -1) {
				if len(id) >= 5 && !seenID[id] && !placeholderRe.MatchString(id) {
					seenID[id] = true
					idents = append(idents, id)
				}
			}
		}
	}
	return idents, paths
}

// relPath returns field as a repo-relative source path worth checking, or
// "" for URLs, absolute paths, git refs and other slash words.
func relPath(field string) string {
	if templated(field) || strings.Contains(field, "://") || strings.HasPrefix(field, "~") || strings.HasPrefix(field, "/") {
		return ""
	}
	if i := strings.IndexAny(field, ":#"); i > 0 {
		field = field[:i] // file.go:42, File.java#L10
	}
	field = strings.TrimPrefix(field, "./")
	if strings.HasPrefix(field, "../") || !codeExtRe.MatchString(field) {
		return ""
	}
	return field
}

// gonePaths flags absolute paths on this machine that no longer exist.
func gonePaths(file, text string) []Suggestion {
	var out []Suggestion
	seen := map[string]bool{}
	for _, loc := range absRe.FindAllStringIndex(text, -1) {
		raw := text[loc[0]:loc[1]]
		if loc[1] < len(text) && strings.ContainsRune("<{*$…", rune(text[loc[1]])) || strings.HasSuffix(raw, "...") {
			continue
		}
		p := strings.TrimRight(raw, ".:")
		if p == "~" || p == "/Users" || seen[p] {
			continue
		}
		seen[p] = true
		if pathExists(p, text[loc[1]:]) {
			continue
		}
		out = append(out, suggest(KindPath, file, p, "no such path on this machine"))
	}
	return out
}

// pathExists stats p, also joined with up to two following words so a path
// with spaces ("~/Library/Application Support/...") isn't cut short.
func pathExists(p, rest string) bool {
	candidate := expandHome(p)
	words := strings.Fields(strings.SplitN(rest, "\n", 2)[0])
	for i := 0; i <= 2 && i <= len(words); i++ {
		if i > 0 {
			candidate += " " + strings.TrimRight(words[i-1], "`'\".,;:)")
		}
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

// goneCommands flags `ox <cmd>` in code spans that the CLI no longer has.
func goneCommands(file, text string, known func([]string) bool) []Suggestion {
	var out []Suggestion
	seen := map[string]bool{}
	for _, sp := range spans(text) {
		for _, m := range oxRe.FindAllStringSubmatch(sp, -1) {
			words := strings.Fields(m[1])
			cmd := "ox " + strings.Join(words, " ")
			if seen[cmd] || known(words) {
				continue
			}
			seen[cmd] = true
			out = append(out, suggest(KindCommand, file, cmd, "ox has no such command"))
		}
	}
	return out
}
