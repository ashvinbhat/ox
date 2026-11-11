package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ashvinbhat/ox/internal/config"
	"github.com/ashvinbhat/ox/internal/stops"
)

func TestAskHookPassesOnlyStops(t *testing.T) {
	defer func(c *config.Config, e error) { cfg, cfgErr = c, e }(cfg, cfgErr)
	home := t.TempDir()
	cfg, cfgErr = &config.Config{Home: home}, nil

	run := func(payload string) (int, string) {
		var stderr bytes.Buffer
		code := askHook(strings.NewReader(payload), &stderr)
		return code, stderr.String()
	}
	cases := []struct {
		name, payload string
		want          int
	}{
		{"another tool", `{"tool_name":"Bash","tool_input":{"command":"ls"}}`, 0},
		{"a box with no question", `{"tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"recap":"done"}}`, 0},
		{"a box asking a go-ahead", `{"session_id":"s1","tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"Commit this?"}}`, 2},
		{"a box asking a send", `{"session_id":"s1","tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"Send: post the reply in #backend?"}}`, 0},
		{"one question of two not a stop", `{"session_id":"s1","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Delete: drop the 3 branches?"},{"question":"Which name?"}]}}`, 2},
		{"unreadable input", `not json`, 0},
	}
	for _, c := range cases {
		code, msg := run(c.payload)
		if code != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, code, c.want)
		}
		if code == 2 && (!strings.Contains(msg, "- Send: ") || !strings.Contains(msg, "Do it, then say what you did")) {
			t.Errorf("%s: sent back without the list: %q", c.name, msg)
		}
	}
	log := stops.Since(home, time.Time{})
	if len(log) != 3 || log[0].Passed || !log[1].Passed || log[1].Kind != "Send" || log[2].Passed {
		t.Fatalf("log = %+v, want the three questions, the send passed", log)
	}

	if err := os.WriteFile(stops.Path(home), []byte("- Spend: money over $20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := run(`{"tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"Spend: a bigger runner?"}}`); code != 0 {
		t.Errorf("a stop from his own list was sent back")
	}
	if code, _ := run(`{"tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"Send: post it?"}}`); code != 2 {
		t.Errorf("a kind he removed from the list still passes")
	}

	cfgErr = errors.New("no ox.yaml")
	if code, _ := run(`{"tool_name":"mcp__ccd_turn__wrap_up","tool_input":{"ask":"Commit this?"}}`); code != 0 {
		t.Errorf("without a config the hook blocked; it must pass")
	}
}

func TestStopsTodayCountsOnlyToday(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	for _, e := range []stops.Entry{
		{At: now.Add(-20 * time.Hour), Passed: true},
		{At: now.Add(-time.Hour), Passed: true},
		{At: now.Add(-time.Hour)},
		{At: now.Add(-time.Minute)},
	} {
		if err := stops.Record(home, e); err != nil {
			t.Fatal(err)
		}
	}
	if got := stopsToday(home, now); got.Asked != 1 || got.SentBack != 2 {
		t.Fatalf("stopsToday = %+v, want 1 asked and 2 sent back today", got)
	}
}
