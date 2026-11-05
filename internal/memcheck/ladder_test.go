package memcheck

import (
	"reflect"
	"testing"
	"time"
)

func TestFeedbackBrokenAgainMovesUpTheLadder(t *testing.T) {
	store := t.TempDir()
	write(t, store, IndexFile, "- [a](a.md)\n- [b](b.md)\n- [c](c.md)\n- [d](d.md)\n")
	front := func(name, typ, meta string) string {
		return "---\nname: " + name + "\ndescription: " + name + "\nmetadata:\n  type: " + typ + "\n" + meta + "---\nA rule about " + name + " and nothing like the others " + name + ".\n"
	}
	// a: broken twice as a memory; b: broken before it moved to its skill, so
	// that miss is spent; c: broken after it became a skill rule; d: a project
	// memory, which has no ladder.
	write(t, store, "a.md", front("alpha", "feedback", "  misses: [2026-10-20, 2026-11-04]\n"))
	write(t, store, "b.md", front("bravo", "feedback", "  rung: skill\n  promoted: 2026-11-01\n  misses: [2026-10-30]\n"))
	write(t, store, "c.md", front("charlie", "feedback", "  rung: skill\n  promoted: 2026-11-01\n  misses: [2026-10-30, 2026-11-05]\n"))
	write(t, store, "d.md", front("delta", "project", "  verified: 2026-11-05\n  misses: [2026-11-05]\n"))

	r, err := Audit(Options{Dir: store, Now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range r.Suggestions {
		if s.Kind == KindMissed {
			got[s.ID] = s.Detail
		}
	}
	want := map[string]string{
		"missed:a.md:2026-11-04": "broken again 2026-11-04 (2 times since it became a memory rule); move it up: a line in the skill whose step it governs, at that step",
		"missed:c.md:2026-11-05": "broken again 2026-11-05 (once since it became a skill rule); move it up: a check in code: an audit check, or a test with a mutations.yaml entry",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}
