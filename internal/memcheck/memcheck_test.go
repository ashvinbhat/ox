package memcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCodeRefs(t *testing.T) {
	text := "Use `StepManifest.stepConfigurationId` and `infer_step_data_layer`, not `MONGO_URL`.\n" +
		"See `.agents/skills/df-guide/SKILL.md`, `internal/harness/ticket.go:42` and `ox.yaml`.\n" +
		"Pattern `findByX` / `GenericResponseXDto`; link `https://x.io/a/b.go`; branch `origin/main`.\n" +
		"```\nnew PublishGroup()\n```\n"
	idents, paths := codeRefs(text)
	wantIdents := []string{"PublishGroup", "StepManifest", "stepConfigurationId", "infer_step_data_layer"}
	if !reflect.DeepEqual(sorted(idents), sorted(wantIdents)) {
		t.Errorf("idents = %v, want %v", idents, wantIdents)
	}
	wantPaths := []string{".agents/skills/df-guide/SKILL.md", "internal/harness/ticket.go"}
	if !reflect.DeepEqual(sorted(paths), sorted(wantPaths)) {
		t.Errorf("paths = %v, want %v", paths, wantPaths)
	}
}

func TestGonePaths(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "Application Support")
	if err := os.Mkdir(spaced, 0o755); err != nil {
		t.Fatal(err)
	}
	text := strings.Join([]string{
		"exists: ~/",
		"gone: ~/.ox-test-surely-missing/x.json.",
		"templated: ~/.ox/worktrees/<repo>/m<seq>",
		"elided: ~/.ox/missions/m167-...",
	}, "\n")
	got := gonePaths("a.md", text)
	if len(got) != 1 || got[0].Subject != "~/.ox-test-surely-missing/x.json" {
		t.Fatalf("gonePaths = %+v", got)
	}
	if !pathExists(strings.Replace(spaced, " Support", "", 1), " Support/ is where") {
		t.Error("path with a space should resolve by joining the next word")
	}
}

func TestGoneCommands(t *testing.T) {
	known := func(words []string) bool { return words[0] == "now" || words[0] == "memory" }
	text := "Run `ox now --brief`, then `ox memory audit`. Old: `cd x && ox morph backend`. Build: `go build -o ox ./cmd/ox`."
	got := goneCommands("a.md", text, known)
	if len(got) != 1 || got[0].Subject != "ox morph backend" {
		t.Fatalf("goneCommands = %+v", got)
	}
}

func TestAuditIndexStaleStrandedKeep(t *testing.T) {
	store := t.TempDir()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	write(t, store, IndexFile, "- [A](a.md) — a\n- [Gone](gone.md) — gone\n")
	write(t, store, "a.md", "---\nname: a\ndescription: d\nmetadata:\n  type: project\n  modified: 2026-09-01T10:00:00.000Z\n---\nBody\n")
	write(t, store, "b.md", "---\nname: b\ndescription: d\nmetadata:\n  type: project\n  verified: 2026-10-20\n---\nBody\n")
	old := now.Add(-60 * 24 * time.Hour)
	for _, f := range []string{"a.md", "b.md"} {
		if err := os.Chtimes(filepath.Join(store, f), old, old); err != nil {
			t.Fatal(err)
		}
	}
	other := t.TempDir()
	write(t, other, "c.md", "---\nname: c\n---\nc\n")

	r, err := Audit(Options{Dir: store, Others: []string{other}, Now: now,
		Keep: map[string]Keep{"index:MEMORY.md:gone.md": {}}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range r.Suggestions {
		ids = append(ids, s.ID)
	}
	want := []string{
		"stranded:" + filepath.Join(other, "c.md"),
		"stale:a.md",          // modified 56 days ago
		"index:b.md:unlisted", // b was verified a week ago, so not stale
	}
	if !reflect.DeepEqual(sorted(ids), sorted(want)) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if r.Kept != 1 {
		t.Errorf("kept = %d, want 1", r.Kept)
	}
}

func TestPendingDropsLaterKeeps(t *testing.T) {
	home := t.TempDir()
	r := Report{At: time.Now(), Suggestions: []Suggestion{{ID: "x"}, {ID: "y"}}}
	if err := SaveReport(home, r); err != nil {
		t.Fatal(err)
	}
	if err := SaveKeep(home, map[string]Keep{"x": {At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	p, err := Pending(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Suggestions) != 1 || p.Suggestions[0].ID != "y" || p.Kept != 1 {
		t.Errorf("pending = %+v", p)
	}
}

// A name deleted from the checkout but still on the default branch is live.
func TestCodeSearchChecksDefaultBranch(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	upstream := t.TempDir()
	run(t, upstream, "git", "init", "-q", "-b", "main")
	write(t, upstream, "src/FooService.go", "type FooService struct{}\n")
	write(t, upstream, "src/BarService.go", "type BarService struct{}\n")
	run(t, upstream, "git", "add", ".")
	run(t, upstream, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	clone := filepath.Join(t.TempDir(), "repo")
	run(t, "", "git", "clone", "-q", upstream, clone)
	if err := os.Remove(filepath.Join(clone, "src/BarService.go")); err != nil {
		t.Fatal(err)
	}

	s := newCodeSearch([]string{clone})
	missing, err := s.missingIdents([]string{"FooService", "BarService", "GoneService"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, []string{"GoneService"}) {
		t.Errorf("missing idents = %v", missing)
	}
	if got := s.missingFiles([]string{"src/FooService.go", "FooService.go", "src/Gone.go"}); !reflect.DeepEqual(got, []string{"src/Gone.go"}) {
		t.Errorf("missing files = %v", got)
	}
	cites := s.goneCites("a.md", []string{"repo:BarService", "repo:src/Gone.go", "other:Foo", "~/.ox-test-surely-missing"})
	var subjects []string
	for _, c := range cites {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"repo:src/Gone.go", "other:Foo", "~/.ox-test-surely-missing"}; !reflect.DeepEqual(subjects, want) {
		t.Errorf("gone cites = %v, want %v", subjects, want)
	}
}

func TestAuditDuplicatesAndBadFrontmatter(t *testing.T) {
	store := t.TempDir()
	write(t, store, IndexFile, "- [a](a.md)\n- [b](b.md)\n- [c](c.md)\n- [d](d.md)\n")
	write(t, store, "a.md", "---\nname: a\ndescription: LinkedIn format quirk\n---\nSet Campaign.format to SINGLE_VIDEO for VIDEO_VIEW and video lead-gen campaigns; otherwise LinkedIn infers it.\n")
	write(t, store, "b.md", "---\nname: b\ndescription: LinkedIn video format\n---\nFor VIDEO_VIEW and video lead-gen, Campaign.format must be SINGLE_VIDEO; LinkedIn infers the rest.\n")
	write(t, store, "c.md", "---\nname: c\ndescription: Temporal test env\n---\nFailure-path workflow tests need temporal.test.env=true or activities retry five times.\n")
	write(t, store, "d.md", "---\nname: d\ndescription: ops: gotchas\n---\nForce-push with a lease and never fetch before the retry.\n")

	r, err := Audit(Options{Dir: store, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range r.Suggestions {
		ids = append(ids, s.ID)
	}
	if want := []string{"duplicate:a.md:b.md", "frontmatter:d.md"}; !reflect.DeepEqual(sorted(ids), want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

// A cite whose file changed on the default branch after the memory was last
// confirmed is flagged; one untouched since is not.
func TestChangedCites(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	upstream := t.TempDir()
	run(t, upstream, "git", "init", "-q", "-b", "main")
	commit := func(date, file, content string) {
		write(t, upstream, file, content)
		run(t, upstream, "git", "add", ".")
		cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", file)
		cmd.Dir = upstream
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
	}
	commit("2026-10-01T10:00:00Z", "src/FooService.go", "type FooService struct{}\n")
	commit("2026-10-01T11:00:00Z", "docs/notes.md", "notes\n")
	commit("2026-10-20T10:00:00Z", "src/FooService.go", "type FooService struct{ n int }\n")
	clone := filepath.Join(t.TempDir(), "repo")
	run(t, "", "git", "clone", "-q", upstream, clone)

	verified := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	local := filepath.Join(t.TempDir(), "tracks.yaml")
	write(t, filepath.Dir(local), "tracks.yaml", "x")
	m := Memory{File: "a.md", Touched: verified, Cites: []string{"repo:FooService", "repo:docs/notes.md", local}}

	var subjects []string
	for _, c := range newCodeSearch([]string{clone}).changedCites(m) {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"repo:FooService", local}; !reflect.DeepEqual(subjects, want) {
		t.Errorf("changed cites = %v, want %v", subjects, want)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
