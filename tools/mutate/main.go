// Command mutate checks that ox's tests guard the rules they are for. Each
// entry in mutations.yaml turns one rule off in a copy of the repo, and the
// tests of that file's package must then fail. A mutation the tests survive,
// one that doesn't compile, or one whose code is gone fails the run. The
// working tree is never touched.
//
//	go run ./tools/mutate            # or: make mutate
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type mutation struct {
	Why  string `yaml:"why"`
	File string `yaml:"file"`
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// skip are folders the copy leaves out: history, other worktrees and
// node_modules aren't needed to build or test.
var skip = map[string]bool{".git": true, ".claude": true, "node_modules": true}

func main() {
	list := flag.String("list", "tools/mutate/mutations.yaml", "the mutations to run")
	flag.Parse()
	if err := run(*list); err != nil {
		fmt.Fprintln(os.Stderr, "mutate:", err)
		os.Exit(1)
	}
}

func run(list string) error {
	data, err := os.ReadFile(list)
	if err != nil {
		return err
	}
	var ms []mutation
	if err := yaml.Unmarshal(data, &ms); err != nil {
		return fmt.Errorf("%s: %w", list, err)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("run from the repo root: %w", err)
	}
	copyDir, err := os.MkdirTemp("", "ox-mutate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(copyDir)
	if err := copyTree(root, copyDir); err != nil {
		return fmt.Errorf("copy the repo: %w", err)
	}

	bad := 0
	for _, m := range ms {
		verdict, why := try(copyDir, m)
		if verdict != "caught" {
			bad++
		}
		fmt.Printf("%-9s %s: %s\n", verdict, m.File, m.Why)
		if why != "" {
			fmt.Printf("          %s\n", why)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d mutations not caught", bad, len(ms))
	}
	fmt.Printf("All %d mutations caught.\n", len(ms))
	return nil
}

// try applies m in dir, runs the package's tests and restores the file. why
// explains a verdict other than caught.
func try(dir string, m mutation) (verdict, why string) {
	path := filepath.Join(dir, m.File)
	orig, err := os.ReadFile(path)
	if err != nil {
		return "missing", err.Error()
	}
	if n := bytes.Count(orig, []byte(m.From)); n != 1 {
		return "stale", fmt.Sprintf("`from` appears %d times; update the entry", n)
	}
	if err := os.WriteFile(path, bytes.Replace(orig, []byte(m.From), []byte(m.To), 1), 0o644); err != nil {
		return "unwritable", err.Error()
	}
	defer os.WriteFile(path, orig, 0o644)

	cmd := exec.Command("go", "test", "-count=1", "./"+filepath.Dir(m.File))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	switch {
	case err == nil:
		return "SURVIVED", "no test failed with this rule off"
	case strings.Contains(string(out), "[build failed]") || strings.Contains(string(out), "[setup failed]"):
		return "no-build", compileError(string(out))
	default:
		return "caught", ""
	}
}

func compileError(out string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, ".go:") {
			return strings.TrimSpace(line)
		}
	}
	return "the mutated package doesn't compile"
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			if skip[d.Name()] && rel != "." {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, info.Mode().Perm())
	})
}
