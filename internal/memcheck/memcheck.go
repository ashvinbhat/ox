// Package memcheck audits the Claude Code auto-memory store, the one place
// memories live: paths, code and ox commands a memory names that no longer
// exist, MEMORY.md drift, project memories nobody has confirmed lately, and
// memories stranded in other projects' stores, cited files that changed
// since a memory was confirmed, and memories that cover the same ground.
package memcheck

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Suggestion kinds.
const (
	KindPath     = "gone-path"
	KindCode     = "gone-code"
	KindCommand  = "gone-command"
	KindCite     = "gone-cite"
	KindIndex    = "index"
	KindStale    = "stale"
	KindStranded = "stranded"
	KindChanged  = "changed"
	KindDup      = "duplicate"
	KindFront    = "frontmatter"
)

// StaleAfter is how long a project memory goes unconfirmed before the audit
// asks whether it still holds. Feedback and references don't age on a clock.
const StaleAfter = 30 * 24 * time.Hour

// IndexFile is the index Claude Code loads into every thread; it reads only
// the first IndexLimit lines.
const (
	IndexFile  = "MEMORY.md"
	IndexLimit = 200
)

// Memory is one memory file.
type Memory struct {
	File        string
	Name        string
	Description string
	Type        string
	Verified    time.Time // metadata.verified: when Ashvin last confirmed it holds
	Touched     time.Time // latest of verified, metadata.modified and the file's mtime
	Cites       []string  // metadata.cites: "repo:path", "repo:Symbol" or "~/path"
	Body        string
	BadFront    bool // frontmatter present but not valid YAML, so its fields are lost
}

// Suggestion is one thing in the store that may need an edit.
type Suggestion struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Subject string `json:"subject,omitempty"`
	Detail  string `json:"detail"`
}

// Report is one audit run.
type Report struct {
	At          time.Time    `json:"at"`
	Dir         string       `json:"dir"`
	Memories    int          `json:"memories"`
	Suggestions []Suggestion `json:"suggestions"`
	Kept        int          `json:"kept,omitempty"`
	Repos       []string     `json:"repos,omitempty"` // searched for code
	Notes       []string     `json:"notes,omitempty"`
}

// Options configures an audit.
type Options struct {
	Dir    string   // the store
	Others []string // other projects' memory dirs
	Repos  []string // checkouts to search for code
	// Known reports whether `ox <words...>` is a real command.
	Known func(words []string) bool
	Keep  map[string]Keep
	Now   time.Time
}

// Audit checks every memory in the store and returns what needs a look,
// minus the suggestions Ashvin already chose to keep.
func Audit(o Options) (Report, error) {
	mems, err := Load(o.Dir)
	if err != nil {
		return Report{}, err
	}
	r := Report{At: o.Now, Dir: o.Dir, Memories: len(mems)}

	all := indexDrift(o.Dir, mems)
	all = append(all, duplicates(mems)...)
	refs := map[string][]string{}  // identifier → memory files naming it
	files := map[string][]string{} // repo-relative path → memory files naming it
	for _, m := range mems {
		if m.BadFront {
			all = append(all, suggest(KindFront, m.File, "",
				"frontmatter isn't valid YAML (quote a description that has a colon), so its type, verified date and cites are ignored"))
		}
		text := m.Description + "\n" + m.Body
		all = append(all, gonePaths(m.File, text)...)
		if o.Known != nil {
			all = append(all, goneCommands(m.File, text, o.Known)...)
		}
		ids, paths := codeRefs(text)
		for _, id := range ids {
			refs[id] = append(refs[id], m.File)
		}
		for _, p := range paths {
			files[p] = append(files[p], m.File)
		}
		if m.Type == "project" && o.Now.Sub(m.Touched) > StaleAfter {
			all = append(all, suggest(KindStale, m.File, "",
				fmt.Sprintf("project memory last touched %s (%d days ago)", m.Touched.Format("2006-01-02"), int(o.Now.Sub(m.Touched).Hours()/24))))
		}
	}

	search := newCodeSearch(o.Repos)
	if search.ready() {
		r.Repos = search.repoNames()
		missing, err := search.missingIdents(keys(refs))
		if err != nil {
			r.Notes = append(r.Notes, err.Error())
		}
		for _, id := range missing {
			for _, f := range refs[id] {
				all = append(all, suggest(KindCode, f, id, "in no repo (gone, or external)"))
			}
		}
		for _, p := range search.missingFiles(keys(files)) {
			for _, f := range files[p] {
				all = append(all, suggest(KindCode, f, p, "no such file in any repo"))
			}
		}
		for _, m := range mems {
			all = append(all, search.goneCites(m.File, m.Cites)...)
			all = append(all, search.changedCites(m)...)
		}
	} else {
		r.Notes = append(r.Notes, search.why)
	}

	for _, dir := range o.Others {
		all = append(all, stranded(dir)...)
	}

	seen := map[string]bool{}
	for _, s := range all {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		if _, ok := o.Keep[s.ID]; ok {
			r.Kept++
			continue
		}
		r.Suggestions = append(r.Suggestions, s)
	}
	sort.SliceStable(r.Suggestions, func(i, j int) bool {
		a, b := r.Suggestions[i], r.Suggestions[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Kind < b.Kind
	})
	return r, nil
}

func suggest(kind, file, subject, detail string) Suggestion {
	id := kind + ":" + file
	if subject != "" {
		id += ":" + subject
	}
	return Suggestion{ID: id, Kind: kind, File: file, Subject: subject, Detail: detail}
}

// Load reads every memory in dir except the index.
func Load(dir string) ([]Memory, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	var mems []Memory
	for _, p := range paths {
		if filepath.Base(p) == IndexFile {
			continue
		}
		m, err := parse(p)
		if err != nil {
			return nil, err
		}
		mems = append(mems, m)
	}
	return mems, nil
}

func parse(path string) (Memory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Memory{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Memory{}, err
	}
	m := Memory{File: filepath.Base(path), Body: string(data), Touched: info.ModTime()}

	front, body, ok := splitFrontmatter(string(data))
	if !ok {
		return m, nil
	}
	var fm struct {
		Name        string
		Description string
		Metadata    struct {
			Type     string
			Modified string
			Verified string
			Cites    []string
		}
	}
	if yaml.Unmarshal([]byte(front), &fm) != nil {
		m.BadFront = true
		return m, nil
	}
	m.Name, m.Description, m.Type, m.Cites, m.Body = fm.Name, fm.Description, fm.Metadata.Type, fm.Metadata.Cites, body
	m.Verified = parseDate(fm.Metadata.Verified)
	for _, t := range []time.Time{m.Verified, parseDate(fm.Metadata.Modified)} {
		if t.After(m.Touched) {
			m.Touched = t
		}
	}
	return m, nil
}

func splitFrontmatter(s string) (front, body string, ok bool) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s, false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return "", s, false
	}
	front = s[4 : 4+end]
	body = strings.TrimPrefix(s[4+end+4:], "\n")
	return front, body, true
}

func parseDate(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

var linkRe = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)

func indexDrift(dir string, mems []Memory) []Suggestion {
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		return []Suggestion{suggest(KindIndex, IndexFile, "", "no index; Claude Code loads memories through MEMORY.md")}
	}
	var out []Suggestion
	if n := strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1; n > IndexLimit {
		out = append(out, suggest(KindIndex, IndexFile, "length",
			fmt.Sprintf("%d lines; Claude Code reads only the first %d", n, IndexLimit)))
	}
	linked := map[string]bool{}
	for _, m := range linkRe.FindAllStringSubmatch(string(data), -1) {
		target := m[1]
		linked[target] = true
		if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
			out = append(out, suggest(KindIndex, IndexFile, target, "links "+target+", which doesn't exist"))
		}
	}
	for _, m := range mems {
		if !linked[m.File] {
			out = append(out, suggest(KindIndex, m.File, "unlisted", "not in MEMORY.md, so no thread sees it"))
		}
	}
	return out
}

func stranded(dir string) []Suggestion {
	mems, err := Load(dir)
	if err != nil {
		return nil
	}
	var out []Suggestion
	for _, m := range mems {
		out = append(out, suggest(KindStranded, filepath.Join(dir, m.File), "",
			"in another project's store, which ox threads never load"))
	}
	return out
}

// DupAt is how alike two memories must be (TF-IDF cosine over their words)
// to be flagged as covering the same ground. Distinct memories in a cleaned
// store stay under 0.25.
const DupAt = 0.4

var wordRe = regexp.MustCompile(`[a-z0-9][a-z0-9_.#/-]*[a-z0-9]`)

// duplicates flags each pair of memories whose words overlap enough that one
// may restate the other. Words common to the whole store weigh nothing.
func duplicates(mems []Memory) []Suggestion {
	counts := make([]map[string]int, len(mems))
	docs := map[string]int{}
	for i, m := range mems {
		counts[i] = map[string]int{}
		for _, w := range wordRe.FindAllString(strings.ToLower(m.Description+"\n"+m.Body), -1) {
			if len(w) >= 3 {
				counts[i][w]++
			}
		}
		for w := range counts[i] {
			docs[w]++
		}
	}
	vecs := make([]map[string]float64, len(mems))
	for i, c := range counts {
		v, norm := map[string]float64{}, 0.0
		for w, n := range c {
			x := (1 + math.Log(float64(n))) * math.Log(float64(len(mems))/float64(docs[w]))
			v[w], norm = x, norm+x*x
		}
		if norm > 0 {
			for w := range v {
				v[w] /= math.Sqrt(norm)
			}
		}
		vecs[i] = v
	}
	var out []Suggestion
	for i := range mems {
		for j := i + 1; j < len(mems); j++ {
			sim := 0.0
			for w, x := range vecs[i] {
				sim += x * vecs[j][w]
			}
			if sim >= DupAt {
				out = append(out, suggest(KindDup, mems[i].File, mems[j].File,
					fmt.Sprintf("%.0f%% alike: may say the same thing; merge them?", sim*100)))
			}
		}
	}
	return out
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// FindStore picks the memory dir threads load: the configured one, else the
// project store holding the most memories. Every other project store is
// returned as well; memories there are stranded.
func FindStore(claudeHome, configured string) (store string, others []string) {
	dirs, _ := filepath.Glob(filepath.Join(claudeHome, "projects", "*", "memory"))
	if configured != "" {
		store = filepath.Clean(expandHome(configured))
	} else {
		best := -1
		for _, d := range dirs {
			if n := countMemories(d); n > best {
				store, best = d, n
			}
		}
	}
	for _, d := range dirs {
		if filepath.Clean(d) != store && countMemories(d) > 0 {
			others = append(others, d)
		}
	}
	return store, others
}

func countMemories(dir string) int {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	n := 0
	for _, p := range paths {
		if filepath.Base(p) != IndexFile {
			n++
		}
	}
	return n
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
