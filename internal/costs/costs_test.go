package costs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Presence must come only from genuinely typed prompts: tool results ride
// user-role lines, meta lines aren't the human, and neither are the
// watcher's own wake-ups.
func TestTailPresenceCountsOnlyTypedPrompts(t *testing.T) {
	lines := `{"type":"user","timestamp":"2026-07-08T10:00:00Z","message":{"content":"real question from the human"}}
{"type":"assistant","timestamp":"2026-07-08T10:00:05Z","message":{"model":"x","usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":10,"cache_read_input_tokens":1000}}}
{"type":"user","timestamp":"2026-07-08T10:05:00Z","message":{"content":[{"tool_use_id":"t1","type":"tool_result","content":"file contents"}]}}
{"type":"user","timestamp":"2026-07-08T10:06:00Z","isMeta":true,"message":{"content":"local command output"}}
{"type":"user","timestamp":"2026-07-08T10:07:00Z","message":{"content":"⚡ ox: 2 mission event(s) need attention — review the attached events and act per your playbook."}}
{"type":"assistant","timestamp":"2026-07-08T10:07:10Z","message":{"model":"x","usage":{"input_tokens":200,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":2000}}}
`
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	delta, ctxTokens, lastUserAt, _, err := Tail(path, 0)
	if err != nil {
		t.Fatal(err)
	}

	want := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	if !lastUserAt.Equal(want) {
		t.Errorf("lastUserAt = %v, want %v (tool_result/meta/wake-up lines must not count)", lastUserAt, want)
	}
	if delta.InputTokens != 300 || delta.OutputTokens != 130 {
		t.Errorf("usage delta = %+v, want input 300 / output 130", delta)
	}
	if ctxTokens != 2200 {
		t.Errorf("contextTokens = %d, want 2200 (last assistant turn)", ctxTokens)
	}
}

func TestTailNoTypedPromptMeansZeroTime(t *testing.T) {
	lines := `{"type":"user","timestamp":"2026-07-08T10:05:00Z","message":{"content":[{"tool_use_id":"t1","type":"tool_result","content":"x"}]}}
`
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, lastUserAt, _, err := Tail(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !lastUserAt.IsZero() {
		t.Errorf("lastUserAt = %v, want zero", lastUserAt)
	}
}
