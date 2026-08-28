package job

import (
	"os"
	"path/filepath"
	"testing"
)

// Real `opencode run --format json` transcript (one step): a step_start, a
// text part, and a step_finish carrying cost. Every event echoes sessionID.
const opencodeTranscript = `{"type":"step_start","timestamp":1787905562752,"sessionID":"ses_abc","part":{"id":"prt_1","messageID":"msg_1","sessionID":"ses_abc","type":"step-start"}}
{"type":"text","timestamp":1787905563814,"sessionID":"ses_abc","part":{"id":"prt_2","messageID":"msg_1","sessionID":"ses_abc","type":"text","text":"pong","time":{"start":1,"end":2}}}
{"type":"step_finish","timestamp":1787905563814,"sessionID":"ses_abc","part":{"id":"prt_3","reason":"stop","messageID":"msg_1","sessionID":"ses_abc","type":"step-finish","tokens":{"total":11987,"input":11908,"output":0,"reasoning":89},"cost":0.00075008}}
`

func writeTranscript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseOpencodeResult(t *testing.T) {
	res, sess, err := parseOpencodeResult(writeTranscript(t, opencodeTranscript))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != "pong" {
		t.Errorf("result = %q, want pong", res.Result)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false")
	}
	if res.TotalCostUSD != 0.00075008 {
		t.Errorf("cost = %v, want 0.00075008", res.TotalCostUSD)
	}
	if sess != "ses_abc" {
		t.Errorf("session = %q, want ses_abc", sess)
	}
}

func TestParseOpencodeResult_MultiStep(t *testing.T) {
	// Two text parts across two steps concatenate; costs sum.
	body := `{"type":"text","sessionID":"ses_x","part":{"type":"text","text":"foo"}}
{"type":"step_finish","sessionID":"ses_x","part":{"type":"step-finish","cost":0.001}}
{"type":"text","sessionID":"ses_x","part":{"type":"text","text":"bar"}}
{"type":"step_finish","sessionID":"ses_x","part":{"type":"step-finish","cost":0.002}}
`
	res, _, err := parseOpencodeResult(writeTranscript(t, body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != "foobar" {
		t.Errorf("result = %q, want foobar", res.Result)
	}
	if res.TotalCostUSD != 0.003 {
		t.Errorf("cost = %v, want 0.003", res.TotalCostUSD)
	}
}

func TestParseOpencodeResult_Empty(t *testing.T) {
	if _, _, err := parseOpencodeResult(writeTranscript(t, "")); err == nil {
		t.Error("expected error for empty transcript")
	}
}

func TestParseOpencodeResult_NoTextIsError(t *testing.T) {
	// Events but no assistant text (e.g. a crash mid-run) is a failure, but
	// any cost incurred is still surfaced so the ledger stays honest.
	body := `{"type":"step_finish","sessionID":"ses_y","part":{"type":"step-finish","cost":0.004}}
`
	res, _, err := parseOpencodeResult(writeTranscript(t, body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !res.IsError {
		t.Error("IsError = false, want true for no-text transcript")
	}
	if res.TotalCostUSD != 0.004 {
		t.Errorf("cost = %v, want 0.004", res.TotalCostUSD)
	}
}
