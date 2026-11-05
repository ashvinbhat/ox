package memcheck

import (
	"fmt"
	"time"
)

// KindMissed is a feedback memory whose rule was broken again after it was
// written or last moved up the ladder: a memory alone didn't hold it.
const KindMissed = "missed"

// nextRung is where a rule goes when it didn't hold where it is. The lowest
// rung that holds wins; metadata.rung says where the rule lives now.
var nextRung = map[string]string{
	"":       "a line in the skill whose step it governs, at that step",
	"memory": "a line in the skill whose step it governs, at that step",
	"skill":  "a check in code: an audit check, or a test with a mutations.yaml entry",
	"check":  "the check missed it; fix the check",
}

// missed flags a feedback memory with misses after its last promotion.
func missed(m Memory) []Suggestion {
	if m.Type != "feedback" {
		return nil
	}
	var last time.Time
	n := 0
	for _, t := range m.Misses {
		if t.After(m.Promoted) {
			n++
			if t.After(last) {
				last = t
			}
		}
	}
	if n == 0 {
		return nil
	}
	where := m.Rung
	if where == "" {
		where = "memory"
	}
	next, ok := nextRung[m.Rung]
	if !ok {
		next = nextRung["memory"]
	}
	return []Suggestion{suggest(KindMissed, m.File, last.Format("2006-01-02"),
		fmt.Sprintf("broken again %s (%s since it became a %s rule); move it up: %s", last.Format("2006-01-02"), times(n), where, next))}
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}
