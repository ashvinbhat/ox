package tracks

import (
	"os"
	"testing"
)

func TestOfPicksFirstMatchingTrack(t *testing.T) {
	ts := []Track{
		{Name: "Durable agents", Match: []string{"durable", "redis", "valkey"}},
		{Name: "Reviews", Short: "review", Match: []string{"review"}},
		{Name: "ox", Match: []string{"ox"}},
	}
	cases := []struct {
		texts []string
		want  string
	}{
		{[]string{"Valkey clusters for durable turn streams (infra)"}, "durable"},
		{[]string{"Slack thread review"}, "review"},
		{[]string{"Durable chat turns PR watch", "review"}, "durable"},
		{[]string{"Test Zerodha Kite API connection"}, ""},
		{[]string{"Ox workflow and context management"}, "ox"},
		{[]string{"Clear the inbox"}, ""},
		{[]string{"Code reviewer prompts"}, "review"},
	}
	for _, c := range cases {
		got := ""
		if tr := Of(ts, c.texts...); tr != nil {
			got = tr.Label()
		}
		if got != c.want {
			t.Errorf("Of(%q) = %q, want %q", c.texts, got, c.want)
		}
	}
}

func TestLoad(t *testing.T) {
	home := t.TempDir()
	if ts, err := Load(home); err != nil || ts != nil {
		t.Fatalf("missing file: got %v, %v; want nil, nil", ts, err)
	}
	body := "tracks:\n  - name: Test layers\n    match: [golden, api-automation]\n"
	if err := os.WriteFile(Path(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ts, err := Load(home)
	if err != nil || len(ts) != 1 || ts[0].Label() != "test" || len(ts[0].Match) != 2 {
		t.Fatalf("got %+v, %v", ts, err)
	}
}
