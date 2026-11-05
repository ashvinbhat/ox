package memcheck

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAlwaysLoadedIsTheProjectsClaudeFilesAndIndex(t *testing.T) {
	claude := t.TempDir()
	root := t.TempDir()
	project := filepath.Join(root, "work", "my.app")
	other := filepath.Join(root, "work", "other")
	store := filepath.Join(claude, "projects", notAlnum.ReplaceAllString(project, "-"), "memory")
	for _, f := range []string{
		filepath.Join(claude, "CLAUDE.md"),
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(root, "work", "CLAUDE.local.md"),
		filepath.Join(other, "CLAUDE.md"),
		filepath.Join(store, IndexFile),
	} {
		writeFile(t, f, "words\n")
	}

	got := AlwaysLoaded(claude, store, []string{other, project + "/"})
	want := []string{
		filepath.Join(claude, "CLAUDE.md"),
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(root, "work", "CLAUDE.local.md"),
		filepath.Join(store, IndexFile),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := AlwaysLoaded(claude, store, []string{other}); len(got) != 2 {
		t.Errorf("no thread in the project: got %q, want only the user CLAUDE.md and the index", got)
	}
}

func TestBudgetNamesTheTotalAndLargestSections(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	index := filepath.Join(dir, IndexFile)
	writeFile(t, claudeMD, "# Rules\none two three four\n\n## Code map\n"+strings.Repeat("word ", 10)+"\n")
	writeFile(t, index, "# Feedback\n"+strings.Repeat("hook\n", IndexLimit+50))
	files := []string{claudeMD, index}

	if got := budget(files, 1000, filepath.Base); got != nil {
		t.Fatalf("under budget: got %v, want nothing", got)
	}
	got := budget(files, 200, filepath.Base)
	// The index counts only the 200 lines Claude Code reads: its heading and 199 hooks.
	want := "every thread loads 220 words, 20 over the 200 budget (CLAUDE.md 19, MEMORY.md 201); " +
		"largest: MEMORY.md › Feedback 201, CLAUDE.md › Code map 13, CLAUDE.md › Rules 6"
	if len(got) != 1 || got[0].ID != KindBudget || got[0].Detail != want {
		t.Fatalf("got %+v\nwant one budget suggestion: %s", got, want)
	}
}

func TestAuditHoldsLoadedTextToTheBudget(t *testing.T) {
	store := t.TempDir()
	writeFile(t, filepath.Join(store, IndexFile), "# Index\n")
	big := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeFile(t, big, strings.Repeat("rule ", WordBudget+1))
	r, err := Audit(Options{Dir: store, Now: time.Now(), Loaded: []string{big, filepath.Join(store, IndexFile)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range r.Suggestions {
		if s.Kind == KindBudget {
			return
		}
	}
	t.Fatalf("no budget suggestion for %d loaded words: %+v", WordBudget+3, r.Suggestions)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
