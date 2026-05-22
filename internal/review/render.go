package review

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"
)

var severityOrder = map[Severity]int{
	SeverityBlocker: 0,
	SeverityIssue:   1,
	SeveritySuggest: 2,
	SeverityNit:     3,
}

// ANSI escape building blocks. No external deps.
const (
	ansiReset      = "\x1b[0m"
	ansiBold       = "\x1b[1m"
	ansiDim        = "\x1b[2m"
	ansiRed        = "\x1b[31m"
	ansiYellow     = "\x1b[33m"
	ansiBrightRed  = "\x1b[91m"
)

// colorize wraps s with the given ANSI sequence iff color output is enabled.
// Honors NO_COLOR (https://no-color.org) and only emits sequences when
// stdout is a terminal.
func colorize(seq, s string) string {
	if !colorEnabled() {
		return s
	}
	return seq + s + ansiReset
}

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if f, ok := outputForColor(); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// outputForColor is overridable in tests; production reads os.Stdout.
var outputForColor = func() (*os.File, bool) { return os.Stdout, true }

func severityColor(sev Severity) string {
	switch sev {
	case SeverityBlocker:
		return ansiBrightRed + ansiBold
	case SeverityIssue:
		return ansiRed
	case SeveritySuggest:
		return ansiYellow
	case SeverityNit:
		return ansiDim
	}
	return ""
}

// TermWidth returns the current terminal width, honoring $COLUMNS, then
// ioctl, with a 100-column fallback.
func TermWidth() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n >= 40 {
			return n
		}
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w >= 40 {
		return w
	}
	return 100
}

func sortFindings(findings []Finding) []Finding {
	out := append([]Finding(nil), findings...)
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := severityOrder[out[i].Severity], severityOrder[out[j].Severity]
		if si != sj {
			return si < sj
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func countBySeverity(findings []Finding) map[Severity]int {
	counts := map[Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	return counts
}

func anchorOf(f Finding) string {
	if f.EndLine > f.Line {
		return fmt.Sprintf("%s:%d-%d", f.File, f.Line, f.EndLine)
	}
	return fmt.Sprintf("%s:%d", f.File, f.Line)
}

// RenderSummary prints one line per finding: severity-coloured header bands,
// numbered titles, with metadata (anchor · category · agent) dimmed.
// Built for scanning — the user picks numbers to expand or post.
func RenderSummary(w io.Writer, findings []Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, colorize(ansiDim, "No findings. ✓"))
		return
	}

	sorted := sortFindings(findings)
	counts := countBySeverity(sorted)

	header := strings.Join([]string{
		colorize(severityColor(SeverityBlocker), fmt.Sprintf("%d blocker", counts[SeverityBlocker])),
		colorize(severityColor(SeverityIssue), fmt.Sprintf("%d issue", counts[SeverityIssue])),
		colorize(severityColor(SeveritySuggest), fmt.Sprintf("%d suggest", counts[SeveritySuggest])),
		colorize(severityColor(SeverityNit), fmt.Sprintf("%d nit", counts[SeverityNit])),
	}, colorize(ansiDim, " · "))
	fmt.Fprintf(w, "\nFindings: %s\n\n", header)

	currentSev := Severity("")
	for i, f := range sorted {
		if f.Severity != currentSev {
			currentSev = f.Severity
			band := strings.ToUpper(string(f.Severity))
			fmt.Fprintf(w, "%s\n", colorize(severityColor(f.Severity)+ansiBold, band))
		}
		idx := colorize(ansiDim, fmt.Sprintf("[%d]", i+1))
		meta := colorize(ansiDim, fmt.Sprintf("(%s · %s · %s)", anchorOf(f), f.Category, f.Agent))
		fmt.Fprintf(w, "  %s %s\n      %s\n", idx, f.Title, meta)
	}
	fmt.Fprintln(w)
}

// RenderOne prints a single finding's expanded body with severity-coloured
// header and word-wrapped paragraph text. Used by the interactive `expand
// <n>` command and by --no-interactive's full dump.
func RenderOne(w io.Writer, f Finding, index int) {
	width := TermWidth()
	rule := strings.Repeat("─", min(width-2, 78))
	fmt.Fprintln(w, colorize(ansiDim, rule))

	sevLabel := colorize(severityColor(f.Severity)+ansiBold, fmt.Sprintf("[%s]", f.Severity))
	idxLabel := colorize(ansiDim, fmt.Sprintf("[%d]", index))
	fmt.Fprintf(w, "%s %s %s\n", idxLabel, sevLabel, colorize(ansiBold, f.Title))
	fmt.Fprintln(w, colorize(ansiDim, fmt.Sprintf("    %s  ·  %s  ·  agent: %s", anchorOf(f), f.Category, f.Agent)))
	fmt.Fprintln(w)

	for _, line := range wrapText(f.Body, max(40, width-4)) {
		fmt.Fprintf(w, "    %s\n", line)
	}
	fmt.Fprintln(w, colorize(ansiDim, rule))
}

// Render is the all-bodies dump used by --no-interactive mode. Prints the
// summary header followed by each finding fully expanded.
func Render(w io.Writer, findings []Finding) {
	if len(findings) == 0 {
		fmt.Fprintln(w, colorize(ansiDim, "No findings. ✓"))
		return
	}
	RenderSummary(w, findings)
	for i, f := range sortFindings(findings) {
		RenderOne(w, f, i+1)
		fmt.Fprintln(w)
	}
}

// wrapText word-wraps s to at most `width` runes per line, treating any
// existing newlines as hard paragraph breaks. Tabs are expanded to two
// spaces. Single-word lines longer than width are emitted on their own line
// unbroken — better than mid-word chopping for paths, code, URLs.
func wrapText(s string, width int) []string {
	if width < 20 {
		width = 20
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.ReplaceAll(para, "\t", "  ")
		if para == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		for _, word := range strings.Fields(para) {
			if line.Len() == 0 {
				line.WriteString(word)
				continue
			}
			if line.Len()+1+len(word) > width {
				out = append(out, line.String())
				line.Reset()
				line.WriteString(word)
				continue
			}
			line.WriteByte(' ')
			line.WriteString(word)
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return out
}

// MaybePager pipes w through $PAGER (or `less`) when w is a terminal and
// the volume of findings is likely to overflow. Returns a writer for the
// caller to write to and a close function that closes the pipe and waits
// for the pager to exit.
//
// LESS=FRX semantics: -F (quit-if-fits, so short output doesn't take over),
// -R (raw ANSI), -X (don't init alt-screen). Inherited from $LESS if set.
func MaybePager(w io.Writer, findingCount int) (io.Writer, func() error) {
	noop := func() error { return nil }
	if findingCount < 5 {
		return w, noop
	}
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return w, noop
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		if _, err := exec.LookPath("less"); err != nil {
			return w, noop
		}
		pager = "less"
	}
	// Build with default flags; users can override via $PAGER directly.
	parts := strings.Fields(pager)
	if len(parts) == 0 {
		return w, noop
	}
	args := parts[1:]
	if filepathBase(parts[0]) == "less" && len(args) == 0 {
		args = []string{"-R", "-F", "-X"}
	}
	cmd := exec.Command(parts[0], args...)
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return w, noop
	}
	if err := cmd.Start(); err != nil {
		return w, noop
	}
	return pipe, func() error {
		_ = pipe.Close()
		return cmd.Wait()
	}
}

func filepathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

