package memcheck

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// codeSearch answers whether names and files exist in the repos. It looks in
// each checkout's working tree first (ripgrep, well under a second a repo),
// then, only for what is still missing, at the tip of the default branch: a
// checkout that is behind must not make live code look gone.
type codeSearch struct {
	repos []repoRef
	rg    string
	why   string // set when the search can't run
}

type repoRef struct{ dir, ref string }

func newCodeSearch(dirs []string) *codeSearch {
	s := &codeSearch{}
	rg, err := exec.LookPath("rg")
	if err != nil {
		s.why = "ripgrep (rg) is not on PATH, so code wasn't checked"
		return s
	}
	s.rg = rg
	for _, d := range dirs {
		s.repos = append(s.repos, repoRef{dir: d, ref: defaultRef(d)})
	}
	if len(s.repos) == 0 {
		s.why = "no repos found, so code wasn't checked"
	}
	return s
}

func (s *codeSearch) ready() bool { return s.why == "" }

// repoNames lists the searched repos by folder name.
func (s *codeSearch) repoNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range s.repos {
		if n := filepath.Base(r.dir); !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// defaultRef is the remote default branch, which may be ahead of the checkout.
func defaultRef(dir string) string {
	if out, err := git(dir, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil && out != "" && out != "origin/HEAD" {
		return out
	}
	for _, ref := range []string{"origin/main", "origin/master"} {
		if _, err := git(dir, "rev-parse", "--verify", "-q", ref); err == nil {
			return ref
		}
	}
	return ""
}

func (s *codeSearch) missingIdents(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	dirs := make([]string, 0, len(s.repos))
	for _, r := range s.repos {
		dirs = append(dirs, r.dir)
	}
	found, err := s.rgFind(ids, dirs)
	missing := subtract(ids, found)
	for _, r := range s.repos {
		if len(missing) == 0 || r.ref == "" {
			continue
		}
		hits, _ := gitGrepFind(r, missing)
		missing = subtract(missing, hits)
	}
	return missing, err
}

// rgFind returns which of ids occur as whole words anywhere under dirs.
func (s *codeSearch) rgFind(ids, dirs []string) (map[string]bool, error) {
	pats, err := patternFile(ids)
	if err != nil {
		return nil, err
	}
	defer os.Remove(pats)
	args := append([]string{"--no-config", "--no-messages", "-F", "-w", "-o", "-I", "-N", "-f", pats, "--"}, dirs...)
	out, err := exec.Command(s.rg, args...).Output()
	// rg exits 1 for no match and 2 when some file was unreadable; both still
	// leave usable output.
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() <= 2) {
		return nil, fmt.Errorf("ripgrep: %w", err)
	}
	return lineSet(out), nil
}

func gitGrepFind(r repoRef, ids []string) (map[string]bool, error) {
	pats, err := patternFile(ids)
	if err != nil {
		return nil, err
	}
	defer os.Remove(pats)
	out, err := exec.Command("git", "-C", r.dir, "grep", "-o", "-h", "-I", "-F", "-w", "-f", pats, r.ref, "--").Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		return nil, err
	}
	return lineSet(out), nil
}

func (s *codeSearch) missingFiles(paths []string) []string {
	missing := paths
	for _, r := range s.repos {
		if len(missing) == 0 {
			continue
		}
		if out, err := git(r.dir, "ls-files"); err == nil {
			missing = subtract(missing, matchFiles(out, missing))
		}
		if len(missing) > 0 && r.ref != "" {
			if out, err := git(r.dir, "ls-tree", "-r", "--name-only", r.ref); err == nil {
				missing = subtract(missing, matchFiles(out, missing))
			}
		}
	}
	return missing
}

// matchFiles returns which of paths name a file in the listing, either from
// the repo root or as a trailing part of a longer path.
func matchFiles(listing string, paths []string) map[string]bool {
	found := map[string]bool{}
	for _, f := range strings.Split(listing, "\n") {
		for _, p := range paths {
			if f == p || strings.HasSuffix(f, "/"+p) {
				found[p] = true
			}
		}
	}
	return found
}

// goneCites checks a memory's explicit citations: "repo:path",
// "repo:Symbol" or an absolute path.
func (s *codeSearch) goneCites(file string, cites []string) []Suggestion {
	var out []Suggestion
	for _, c := range cites {
		if strings.HasPrefix(c, "~") || strings.HasPrefix(c, "/") {
			if !pathExists(c, "") {
				out = append(out, suggest(KindCite, file, c, "cited path no longer exists"))
			}
			continue
		}
		name, spec, ok := strings.Cut(c, ":")
		if !ok || spec == "" {
			out = append(out, suggest(KindCite, file, c, `cite as "repo:path", "repo:Symbol" or "~/path"`))
			continue
		}
		sub := &codeSearch{rg: s.rg}
		for _, r := range s.repos {
			if filepath.Base(r.dir) == name {
				sub.repos = append(sub.repos, r)
			}
		}
		if len(sub.repos) == 0 {
			out = append(out, suggest(KindCite, file, c, "no checkout of "+name+" to check against"))
			continue
		}
		var gone bool
		if strings.Contains(spec, "/") || fileExtRe.MatchString(spec) {
			gone = len(sub.missingFiles([]string{spec})) > 0
		} else {
			missing, err := sub.missingIdents([]string{spec})
			gone = err == nil && len(missing) > 0
		}
		if gone {
			out = append(out, suggest(KindCite, file, c, "cited code is gone from "+name))
		}
	}
	return out
}

// changedCites reports cited files with commits on the default branch since
// the memory was last confirmed or edited: the file is still there, but what
// the memory says about it may no longer hold. A symbol cite stands for the
// files named after it (SegmentService.java).
func (s *codeSearch) changedCites(m Memory) []Suggestion {
	if m.Touched.IsZero() {
		return nil
	}
	since := m.Touched.Format("2006-01-02")
	var out []Suggestion
	for _, c := range m.Cites {
		if strings.HasPrefix(c, "~") || strings.HasPrefix(c, "/") {
			if info, err := os.Stat(expandHome(c)); err == nil && info.ModTime().After(m.Touched) {
				out = append(out, suggest(KindChanged, m.File, c,
					fmt.Sprintf("edited %s, after the memory was last confirmed or edited (%s)", info.ModTime().Format("2006-01-02"), since)))
			}
			continue
		}
		name, spec, ok := strings.Cut(c, ":")
		if !ok || spec == "" {
			continue
		}
		for _, r := range s.repos {
			if filepath.Base(r.dir) != name {
				continue
			}
			ref := r.ref
			if ref == "" {
				ref = "HEAD"
			}
			listing, err := git(r.dir, "ls-tree", "-r", "--name-only", ref)
			if err != nil {
				continue
			}
			files := citedFiles(listing, spec)
			if len(files) == 0 {
				break
			}
			n, err := git(r.dir, append([]string{"rev-list", "--count", "--since=" + m.Touched.Format(time.RFC3339), ref, "--"}, files...)...)
			if err == nil && n != "0" {
				out = append(out, suggest(KindChanged, m.File, c,
					fmt.Sprintf("%s %s on %s since the memory was last confirmed or edited (%s)", n, plural(n, "commit"), ref, since)))
			}
			break
		}
	}
	return out
}

// citedFiles lists the files in a tree listing that a cite names: the path
// itself, or for a symbol every file whose name without extension is it.
func citedFiles(listing, spec string) []string {
	isFile := strings.Contains(spec, "/") || fileExtRe.MatchString(spec)
	var out []string
	for _, f := range strings.Split(listing, "\n") {
		base := path.Base(f)
		if isFile && (f == spec || strings.HasSuffix(f, "/"+spec)) ||
			!isFile && strings.TrimSuffix(base, path.Ext(base)) == spec {
			out = append(out, f)
		}
	}
	return out
}

func plural(n, word string) string {
	if n == "1" {
		return word
	}
	return word + "s"
}

func patternFile(ids []string) (string, error) {
	f, err := os.CreateTemp("", "ox-memcheck-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(ids, "\n") + "\n"); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func lineSet(out []byte) map[string]bool {
	set := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		set[sc.Text()] = true
	}
	return set
}

func subtract(list []string, drop map[string]bool) []string {
	var out []string
	for _, s := range list {
		if !drop[s] {
			out = append(out, s)
		}
	}
	return out
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
