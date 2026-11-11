package stops

import (
	"os"
	"testing"
	"time"
)

func TestKindOfNeedsAKindFromTheList(t *testing.T) {
	list := Parse(defaultList)
	if len(list) != 5 {
		t.Fatalf("default list = %v, want 5 stops", list)
	}
	cases := map[string]string{
		"Send: post this reply in #backend?":     "Send",
		"  delete: remove the 3 stale branches?": "Delete",
		"Commit this?":                           "",
		"Should I merge: ox#45?":                 "",
		"Spend: $40 on a bigger runner?":         "",
	}
	for q, want := range cases {
		if got := KindOf(q, list); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", q, got, want)
		}
	}
}

func TestLoadUsesHisListElseTheDefault(t *testing.T) {
	home := t.TempDir()
	if list, _ := Load(home); len(list) != 5 {
		t.Fatalf("no file: %v, want the default", list)
	}
	if err := os.WriteFile(Path(home), []byte("- Spend: money over $20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if list, _ := Load(home); len(list) != 1 || KindOf("Spend: a bigger runner?", list) != "Spend" {
		t.Fatalf("his list: %v", list)
	}
	if err := os.WriteFile(Path(home), []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if list, _ := Load(home); len(list) != 5 {
		t.Fatalf("a list naming no stop: %v, want the default", list)
	}
}

func TestInstallKeepsHisList(t *testing.T) {
	home := t.TempDir()
	if wrote, err := Install(home); !wrote || err != nil {
		t.Fatalf("first install: %v, %v", wrote, err)
	}
	if err := os.WriteFile(Path(home), []byte("- Spend: money\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wrote, err := Install(home); wrote || err != nil {
		t.Fatalf("second install: %v, %v", wrote, err)
	}
	if b, _ := os.ReadFile(Path(home)); string(b) != "- Spend: money\n" {
		t.Fatalf("his list was overwritten: %q", b)
	}
}

func TestSinceReadsTheLogFromATime(t *testing.T) {
	home := t.TempDir()
	day := time.Date(2026, 11, 11, 0, 0, 0, 0, time.Local)
	for i, at := range []time.Time{day.Add(-time.Hour), day.Add(time.Hour), day.Add(2 * time.Hour)} {
		if err := Record(home, Entry{At: at, Session: "s", Question: "q", Passed: i == 2}); err != nil {
			t.Fatal(err)
		}
	}
	got := Since(home, day)
	if len(got) != 2 || got[0].Passed || !got[1].Passed {
		t.Fatalf("Since = %+v, want today's two, oldest first", got)
	}
}
