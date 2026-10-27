package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestState(t *testing.T) {
	now := time.UnixMilli(1_793_000_000_000)
	ago := func(d time.Duration) float64 { return float64(now.Add(-d).UnixMilli()) }

	cases := []struct {
		name string
		s    Session
		want State
	}{
		{"open question", Session{LastActiveMs: ago(5 * time.Hour), Summary: &Summary{Status: "blocked", NeedsAction: "Is a move to GCP planned?"}}, StateNeedsYou},
		{"blocked without text", Session{LastActiveMs: ago(time.Minute), Summary: &Summary{Status: "blocked"}}, StateNeedsYou},
		{"interrupted", Session{LastActiveMs: ago(time.Minute), Interrupted: json.RawMessage(`{"at":1}`)}, StateNeedsYou},
		{"turn running", Session{LastActiveMs: ago(2 * time.Minute)}, StateWorking},
		{"turn died", Session{LastActiveMs: ago(3 * time.Hour), Interrupted: json.RawMessage(`null`)}, StateStuck},
		{"open PR", Session{LastActiveMs: ago(time.Hour), Summary: &Summary{Status: "completed"}, PRs: []PR{{Number: 9670, State: "OPEN"}}}, StateInReview},
		{"review ready", Session{LastActiveMs: ago(time.Hour), Summary: &Summary{Status: "review_ready"}}, StateInReview},
		{"merged PR", Session{LastActiveMs: ago(time.Hour), Summary: &Summary{Status: "completed"}, PRs: []PR{{Number: 2195, State: "MERGED"}}}, StateDone},
		{"finished, no PR", Session{LastActiveMs: ago(time.Hour), Summary: &Summary{Status: "completed"}}, StateDone},
	}
	for _, c := range cases {
		if got := c.s.State(now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestArchivable(t *testing.T) {
	now := time.UnixMilli(1_793_000_000_000)
	done := Session{Summary: &Summary{Status: "completed"}}
	done.LastActiveMs = float64(now.Add(-72 * time.Hour).UnixMilli())
	if !done.Archivable(now) {
		t.Error("done thread idle 3 days should be archivable")
	}
	done.LastActiveMs = float64(now.Add(-time.Hour).UnixMilli())
	if done.Archivable(now) {
		t.Error("done thread idle 1 hour should not be archivable")
	}
}

func TestListSkipsArchivedDeletedAndUnparseable(t *testing.T) {
	dir := t.TempDir()
	org := filepath.Join(dir, "acct", "org")
	if err := os.MkdirAll(org, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(org, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("local_a.json", `{"sessionId":"local_a","title":"older","lastActivityAt":100,"someNewField":{"x":1}}`)
	write("local_b.json", `{"sessionId":"local_b","title":"newer","lastActivityAt":200}`)
	write("local_c.json", `{"sessionId":"local_c","isArchived":true,"lastActivityAt":300}`)
	write("local_d.json", `{not json`)
	write("deleted_e.json", `{"sessionId":"deleted_e","lastActivityAt":400}`)

	got, err := List(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "newer" || got[1].Title != "older" {
		t.Fatalf("got %+v, want [newer older]", got)
	}

	all, _ := List(dir, true)
	if len(all) != 3 {
		t.Fatalf("with archived: got %d threads, want 3", len(all))
	}

	none, err := List(filepath.Join(dir, "missing"), false)
	if err != nil || len(none) != 0 {
		t.Fatalf("missing dir: got %v, %v; want empty, nil", none, err)
	}
}
