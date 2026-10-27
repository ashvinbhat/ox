package memcheck

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
